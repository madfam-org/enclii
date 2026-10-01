package api

// providers.cloudflare.tunnels-apply: reconcile a project's junction tunnel
// routes against the live tunnel ingress.
//
// The plan is the contract. The dry run and the apply build it the same way
// (planJunctionTunnelRoutes), and the apply refuses to run when:
//
//   - any row in scope is blocked by the repoint guard (tunnel_route_guard.go):
//     an apply is all-or-nothing, so what an operator reviewed is either
//     executed whole or not at all;
//   - --expect-plan names a plan that is no longer the current one.
//
// The backend each junction should reach is derived from the junction's
// service AND environment (junction_route_target.go), not from the project
// namespace.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type tunnelRoutePlanItem struct {
	Hostname string `json:"hostname"`
	// Action is create, update, skip, repoint or blocked. Label is the same
	// decision as an operator reads it, e.g. "REPOINT (blocked)".
	Action  string `json:"action"`
	Label   string `json:"label"`
	Blocked bool   `json:"blocked"`
	// Guard names the rule that refused or reclassified the row.
	Guard             string `json:"guard,omitempty"`
	CurrentService    string `json:"current_service,omitempty"`
	CurrentHealth     string `json:"current_health,omitempty"`
	DesiredService    string `json:"desired_service"`
	ServiceName       string `json:"service_name"`
	Namespace         string `json:"namespace"`
	Port              int    `json:"port"`
	Environment       string `json:"environment,omitempty"`
	EnvironmentSource string `json:"environment_source,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

// executable reports whether an apply would write this row.
func (item tunnelRoutePlanItem) executable() bool {
	if item.Blocked {
		return false
	}
	switch item.Action {
	case tunnelActionCreate, tunnelActionUpdate, tunnelActionRepoint:
		return true
	}
	return false
}

func planTunnelRouteDrifts(live []services.IngressRule, specs []*services.RouteSpec) []tunnelRoutePlanItem {
	items := make([]tunnelRoutePlanItem, 0, len(specs))
	for _, spec := range specs {
		if spec == nil || strings.TrimSpace(spec.Hostname) == "" {
			continue
		}
		want := tunnelRouteServiceURL(spec)
		current := currentTunnelService(live, spec.Hostname)
		item := tunnelRoutePlanItem{
			Hostname:       spec.Hostname,
			DesiredService: want,
			ServiceName:    spec.ServiceName,
			Namespace:      spec.ServiceNamespace,
			Port:           spec.ServicePort,
			CurrentService: current,
		}
		switch {
		case current == "":
			item.Action = tunnelActionCreate
			item.Reason = "tunnel route missing"
		case current == want || sameClusterBackend(current, spec):
			item.Action = tunnelActionSkip
			item.Reason = "already targets desired service"
		default:
			item.Action = tunnelActionUpdate
			item.Reason = "live route targets different backend"
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Hostname < items[j].Hostname })
	return items
}

func currentTunnelService(live []services.IngressRule, hostname string) string {
	for _, route := range live {
		if strings.EqualFold(strings.TrimSpace(route.Hostname), strings.TrimSpace(hostname)) {
			return route.Service
		}
	}
	return ""
}

// tunnelPlanRequest is what both halves of tunnels-apply plan from.
type tunnelPlanRequest struct {
	Project      string
	Target       string
	AllowRepoint map[string]bool
}

func tunnelPlanRequestFrom(req operatorOperationRequest) tunnelPlanRequest {
	project := strings.TrimSpace(req.Scope["project"])
	if project == "" {
		project = strings.TrimSpace(req.Args["project"])
	}
	allow := map[string]bool{}
	for _, host := range strings.FieldsFunc(operationArg(req, "allow_repoint", "allow-repoint"), func(r rune) bool {
		return r == ',' || r == ' '
	}) {
		if host = canonicalDomain(host); host != "" {
			allow[host] = true
		}
	}
	return tunnelPlanRequest{Project: project, Target: canonicalDomain(operationTarget(req)), AllowRepoint: allow}
}

func (h *Handler) planJunctionTunnelRoutes(ctx context.Context, planReq tunnelPlanRequest) ([]tunnelRoutePlanItem, *types.Project, error) {
	if h == nil || h.repos == nil || h.repos.Projects == nil || h.repos.Junctions == nil || h.repos.Services == nil {
		return nil, nil, fmt.Errorf("project repositories are not configured")
	}
	if h.tunnelRoutesService == nil {
		return nil, nil, fmt.Errorf("cloudflare tunnel routes service is not configured")
	}
	if planReq.Project == "" {
		return nil, nil, fmt.Errorf("project slug is required")
	}

	project, err := h.repos.Projects.GetBySlug(planReq.Project)
	if err != nil {
		return nil, nil, fmt.Errorf("project lookup failed: %w", err)
	}
	if project == nil {
		return nil, nil, fmt.Errorf("project %q not found", planReq.Project)
	}

	if _, err := h.ensureDefaultProductionEnvironment(ctx, project); err != nil {
		h.logger.Warn(ctx, "Failed to ensure default environment before tunnel route planning",
			logging.String("project", project.Slug),
			logging.Error("error", err))
	}

	junctions, err := h.repos.Junctions.ListByProject(ctx, project.ID)
	if err != nil {
		return nil, project, fmt.Errorf("list junctions: %w", err)
	}

	envs := h.loadProjectEnvironments(project)
	specs := make([]*services.RouteSpec, 0, len(junctions))
	resolved := map[string]junctionEnvironment{}
	for _, junction := range junctions {
		if junction == nil || junction.Domain == "" {
			continue
		}
		if planReq.Target != "" && canonicalDomain(junction.Domain) != planReq.Target {
			continue
		}

		service, err := h.repos.Services.GetByID(junction.ServiceID)
		if err != nil {
			return nil, project, fmt.Errorf("service lookup for %s: %w", junction.Domain, err)
		}
		env := h.resolveJunctionEnvironment(ctx, junction, envs)
		namespace := h.namespaceForEnvironment(ctx, project, service, env.Name, envs)
		resolved[junction.Domain] = env
		specs = append(specs, &services.RouteSpec{
			Hostname:         junction.Domain,
			ServiceName:      service.Name,
			ServiceNamespace: namespace,
			ServicePort:      h.resolveTunnelBackendPort(ctx, service, namespace, 0),
		})
	}

	live, err := h.tunnelRoutesService.ListRoutes(ctx)
	if err != nil {
		return nil, project, fmt.Errorf("list tunnel routes: %w", err)
	}

	guard := tunnelRouteGuard{
		envs:         envs,
		allowRepoint: planReq.AllowRepoint,
		liveHealth:   h.liveTunnelBackendHealth,
		resolve:      h.resolveTunnelBackend,
	}
	plan := planTunnelRouteDrifts(live, specs)
	for i := range plan {
		env := resolved[plan[i].Hostname]
		plan[i].Environment = env.Name
		plan[i].EnvironmentSource = env.Source
		guard.evaluate(ctx, &plan[i])
		if env.Note != "" {
			plan[i].Reason = joinNotes(plan[i].Reason, env.Note)
		}
	}
	return plan, project, nil
}

// tunnelPlanTally is the plan reduced to what a summary and a gate need.
type tunnelPlanTally struct {
	Executable  []tunnelRoutePlanItem
	Blocked     []tunnelRoutePlanItem
	Fingerprint string
}

func tallyTunnelPlan(plan []tunnelRoutePlanItem) tunnelPlanTally {
	tally := tunnelPlanTally{}
	hash := sha256.New()
	for _, item := range plan {
		switch {
		case item.Blocked:
			tally.Blocked = append(tally.Blocked, item)
		case item.executable():
			tally.Executable = append(tally.Executable, item)
			fmt.Fprintf(hash, "%s|%s|%s\n", item.Hostname, item.Action, item.DesiredService)
		}
	}
	if len(tally.Executable) > 0 {
		tally.Fingerprint = hex.EncodeToString(hash.Sum(nil))[:12]
	}
	return tally
}

func hostnamesOf(items []tunnelRoutePlanItem) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Hostname)
	}
	return strings.Join(names, ", ")
}

// blockedSummary is the sentence that leads every response with a blocked row.
// It comes FIRST: the operator who reads one line must read this one.
func blockedSummary(blocked []tunnelRoutePlanItem) string {
	repoints := 0
	for _, item := range blocked {
		if item.Label == tunnelLabelRepointBlocked {
			repoints++
		}
	}
	return fmt.Sprintf("REFUSED: %d row(s) blocked (%d REPOINT (blocked)): %s",
		len(blocked), repoints, hostnamesOf(blocked))
}

// blockedNext suggests, per blocked hostname, the DRY-RUN that previews
// binding the junction to the backend that is serving it now. Only dry runs
// are suggested: the operator decides from the preview whether that binding
// is right.
func blockedNext(project string, blocked []tunnelRoutePlanItem, envs projectEnvironments) []string {
	next := []string{}
	for _, item := range blocked {
		live, ok := parseClusterBackend(item.CurrentService)
		if !ok {
			continue
		}
		env := envs.environmentOfNamespace(live.Namespace)
		if env == "" {
			env = item.Environment
		}
		next = append(next, fmt.Sprintf("enclii ops junctions rebind %s --project %s --to-service %s --environment %s",
			item.Hostname, project, live.Service, env))
	}
	return next
}

func tunnelsApplySteps() []operatorOperationStep {
	return []operatorOperationStep{
		{Name: "authorize", Status: "planned", Detail: "check caller RBAC and require reason on apply"},
		{Name: "load-state", Status: "planned", Detail: "load project junctions, their environments, and live Cloudflare tunnel routes"},
		{Name: "diff", Status: "planned", Detail: "compare each junction's service+environment backend with live tunnel ingress"},
		{Name: "guard", Status: "planned", Detail: "refuse repoints of serving routes, cross-environment moves, and unresolvable backends"},
		{Name: "audit", Status: "planned", Detail: "record operation reason and idempotency key before mutation"},
	}
}

func (h *Handler) handleProviderCloudflareTunnelsApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	planReq := tunnelPlanRequestFrom(req)
	data := map[string]any{"project": planReq.Project, "target": planReq.Target}
	resp := operatorOperationResponse{
		OperationID: operationID, Operation: operation, DryRun: true, Data: data, Steps: tunnelsApplySteps(),
	}

	if planReq.Project == "" {
		resp.Status = "invalid_request"
		resp.Summary = "cloudflare.tunnels-apply requires scope.project"
		resp.Warnings = []string{"missing scope.project or --project"}
		return resp
	}
	if h.tunnelRoutesService == nil {
		resp.Status = "adapter_unconfigured"
		resp.Summary = "cloudflare.tunnels-apply cannot run until the Cloudflare tunnel routes service is configured"
		resp.Warnings = []string{"cloudflare tunnel routes service is not configured"}
		resp.Next = []string{"set ENCLII_CLOUDFLARE_TUNNEL_ID and tunnel route wiring on switchyard-api"}
		return resp
	}

	plan, project, err := h.planJunctionTunnelRoutes(ctx, planReq)
	if err != nil {
		resp.Status = "failed"
		if strings.Contains(err.Error(), "not found") {
			resp.Status = "invalid_request"
		}
		resp.Summary = fmt.Sprintf("cloudflare.tunnels-apply planning failed for project %s", planReq.Project)
		resp.Warnings = []string{err.Error()}
		return resp
	}

	tally := tallyTunnelPlan(plan)
	resp.Data = map[string]any{
		"project":          project.Slug,
		"target":           planReq.Target,
		"plan":             plan,
		"count":            len(tally.Executable),
		"blocked":          len(tally.Blocked),
		"plan_fingerprint": tally.Fingerprint,
	}
	scope := ""
	if planReq.Target != "" {
		scope = planReq.Target + " "
	}

	switch {
	case len(tally.Blocked) > 0:
		resp.Status = "blocked"
		resp.Summary = fmt.Sprintf("%s. An apply of this plan is refused as a whole; %d other row(s) would be written.",
			blockedSummary(tally.Blocked), len(tally.Executable))
		for _, item := range tally.Blocked {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s %s: %s", item.Label, item.Hostname, item.Reason))
		}
		resp.Next = blockedNext(project.Slug, tally.Blocked, h.loadProjectEnvironments(project))
	case len(tally.Executable) == 0:
		resp.Status = "succeeded"
		resp.Summary = fmt.Sprintf("tunnel routes already target desired services for project %s", project.Slug)
		if len(plan) == 0 {
			resp.Summary = fmt.Sprintf("no junction domains found for project %s", project.Slug)
		}
	default:
		resp.Status = "ready_to_apply"
		resp.Summary = fmt.Sprintf("plan to reconcile %d tunnel route(s) for project %s (plan %s)",
			len(tally.Executable), project.Slug, tally.Fingerprint)
		resp.Next = []string{fmt.Sprintf(
			"enclii providers cloudflare tunnels-apply %s--project %s --expect-plan %s --apply --reason \"reconcile junction tunnel routes\"",
			scope, project.Slug, tally.Fingerprint)}
	}
	return resp
}

func (h *Handler) handleProviderCloudflareTunnelsApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	planReq := tunnelPlanRequestFrom(req)
	resp := operatorOperationResponse{OperationID: operationID, Operation: operation, DryRun: false}

	if planReq.Project == "" {
		resp.Status = "invalid_request"
		resp.Summary = "cloudflare.tunnels-apply requires scope.project"
		resp.Warnings = []string{"missing scope.project or --project"}
		return resp, http.StatusBadRequest
	}
	if h.tunnelRoutesService == nil {
		resp.Status = "adapter_unconfigured"
		resp.Summary = "cloudflare.tunnels-apply cannot run until the Cloudflare tunnel routes service is configured"
		resp.Warnings = []string{"cloudflare tunnel routes service is not configured"}
		return resp, http.StatusServiceUnavailable
	}

	plan, project, err := h.planJunctionTunnelRoutes(ctx, planReq)
	if err != nil {
		code := http.StatusInternalServerError
		resp.Status = "failed"
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusBadRequest
			resp.Status = "invalid_request"
		}
		resp.Summary = fmt.Sprintf("cloudflare.tunnels-apply failed for project %s", planReq.Project)
		resp.Warnings = []string{err.Error()}
		return resp, code
	}

	tally := tallyTunnelPlan(plan)
	if len(tally.Blocked) > 0 {
		resp.Status = "blocked"
		resp.Summary = blockedSummary(tally.Blocked) + ". Nothing was applied."
		resp.Data = map[string]any{"project": project.Slug, "plan": plan, "blocked": len(tally.Blocked)}
		for _, item := range tally.Blocked {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s %s: %s", item.Label, item.Hostname, item.Reason))
		}
		resp.Next = blockedNext(project.Slug, tally.Blocked, h.loadProjectEnvironments(project))
		return resp, http.StatusConflict
	}
	if expected := operationArg(req, "expect_plan", "expect-plan"); expected != "" && expected != tally.Fingerprint {
		resp.Status = "plan_changed"
		resp.Summary = fmt.Sprintf("REFUSED: the plan is %q now, not the reviewed %q. Nothing was applied; re-run the dry run.",
			tally.Fingerprint, expected)
		resp.Data = map[string]any{"project": project.Slug, "plan": plan, "plan_fingerprint": tally.Fingerprint}
		return resp, http.StatusConflict
	}

	created, updated, skipped, warnings := h.executeTunnelPlan(ctx, plan)
	mutated := len(created) + len(updated)
	resp.Status, resp.Summary = "submitted", fmt.Sprintf("reconciled %d tunnel route(s) for project %s through Enclii", mutated, project.Slug)
	code := http.StatusAccepted
	if mutated == 0 && len(warnings) > 0 {
		resp.Status, resp.Summary, code = "failed", "no tunnel routes were reconciled", http.StatusInternalServerError
	} else if mutated == 0 {
		resp.Status, code = "succeeded", http.StatusOK
		resp.Summary = fmt.Sprintf("tunnel routes already target desired services for project %s", project.Slug)
	}
	resp.Data = map[string]any{
		"project": project.Slug, "created": created, "updated": updated, "skipped": skipped,
		"plan_fingerprint": tally.Fingerprint,
	}
	resp.Warnings = warnings
	return resp, code
}

// executeTunnelPlan writes every executable row. Only called on a plan with no
// blocked rows.
func (h *Handler) executeTunnelPlan(ctx context.Context, plan []tunnelRoutePlanItem) (created, updated, skipped, warnings []string) {
	created, updated, skipped, warnings = []string{}, []string{}, []string{}, []string{}
	for _, item := range plan {
		if !item.executable() {
			skipped = append(skipped, item.Hostname)
			continue
		}
		spec := &services.RouteSpec{
			Hostname:         item.Hostname,
			ServiceName:      item.ServiceName,
			ServiceNamespace: item.Namespace,
			ServicePort:      item.Port,
		}
		if err := h.tunnelRoutesService.AddRoute(ctx, spec); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", item.Hostname, err))
			continue
		}
		if item.Action == tunnelActionCreate {
			created = append(created, item.Hostname)
		} else {
			updated = append(updated, item.Hostname)
		}
	}
	return created, updated, skipped, warnings
}
