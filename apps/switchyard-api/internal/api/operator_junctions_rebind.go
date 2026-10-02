package api

// ops.junctions.rebind: bind a hostname's junction to the service AND
// environment that serve it.
//
// Junctions were bound by whichever path created them first, and the
// single-document push reconcile bound every hostname of a multi-service
// project to one service. Nothing could correct that afterwards: the junction
// API has create and delete, and delete tears down the hostname's DNS record
// and tunnel route. This is the correction, as an operator operation:
//
//   - dry run by default; --apply --reason executes;
//   - idempotent: a junction already bound as asked is reported and left alone;
//   - data only: it rewrites the binding and never touches the tunnel route.
//     The route follows through `tunnels-apply`, whose repoint guard still
//     applies, so a wrong rebind cannot move live traffic by itself;
//   - fails closed on anything it cannot resolve (unknown project, hostname,
//     service or environment), naming what does exist.

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// junctionRebindRow is one junction's planned (or applied) binding change.
type junctionRebindRow struct {
	JunctionID         string `json:"junction_id"`
	Hostname           string `json:"hostname"`
	Path               string `json:"path"`
	Action             string `json:"action"` // rebind | noop
	CurrentService     string `json:"current_service"`
	CurrentEnvironment string `json:"current_environment"`
	CurrentEnvSource   string `json:"current_environment_source"`
	TargetService      string `json:"target_service"`
	TargetEnvironment  string `json:"target_environment"`
	// The backend tunnels-apply will want once this binding is in place, and
	// whether the live route already serves it. live_matches=true means the
	// follow-up tunnels-apply is a SKIP for this hostname.
	BackendAfter string `json:"backend_after"`
	LiveBackend  string `json:"live_backend,omitempty"`
	LiveMatches  bool   `json:"live_matches"`
}

// junctionRebindPlan is everything both halves need, resolved once.
type junctionRebindPlan struct {
	Project     *types.Project
	Service     *types.Service
	Environment *types.Environment
	Junctions   []*types.Junction
	Rows        []junctionRebindRow
}

func (p junctionRebindPlan) changes() int {
	n := 0
	for _, row := range p.Rows {
		if row.Action == "rebind" {
			n++
		}
	}
	return n
}

// junctionRebindArgs reads the operation's inputs.
func junctionRebindArgs(req operatorOperationRequest) (project, hostname, serviceName, envName string) {
	project = strings.TrimSpace(req.Scope["project"])
	if project == "" {
		project = operationArg(req, "project")
	}
	hostname = canonicalDomain(operationTarget(req))
	if hostname == "" {
		hostname = canonicalDomain(operationArg(req, "hostname", "domain"))
	}
	serviceName = operationArg(req, "to_service", "to-service", "service")
	envName = operationArg(req, "environment", "env")
	return project, hostname, serviceName, envName
}

// planJunctionRebind resolves and validates a rebind. Every refusal is an
// invalid_request error naming the fix.
func (h *Handler) planJunctionRebind(ctx context.Context, req operatorOperationRequest) (junctionRebindPlan, error) {
	plan := junctionRebindPlan{}
	projectSlug, hostname, serviceName, envName := junctionRebindArgs(req)
	missing := []string{}
	for name, value := range map[string]string{
		"--project": projectSlug, "<hostname>": hostname, "--to-service": serviceName, "--environment": envName,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return plan, fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	if h == nil || h.repos == nil || h.repos.Projects == nil || h.repos.Junctions == nil ||
		h.repos.Services == nil || h.repos.Environments == nil {
		return plan, fmt.Errorf("project repositories are not configured")
	}

	project, err := h.repos.Projects.GetBySlug(projectSlug)
	if err != nil || project == nil {
		return plan, fmt.Errorf("project %q not found", projectSlug)
	}
	plan.Project = project

	junctions, err := h.repos.Junctions.ListByProject(ctx, project.ID)
	if err != nil {
		return plan, fmt.Errorf("list junctions: %w", err)
	}
	path := operationArg(req, "path")
	for _, junction := range junctions {
		if junction != nil && canonicalDomain(junction.Domain) == hostname && (path == "" || junction.Path == path) {
			plan.Junctions = append(plan.Junctions, junction)
		}
	}
	if len(plan.Junctions) == 0 {
		return plan, fmt.Errorf("project %s has no junction for %s; nothing to rebind", project.Slug, hostname)
	}

	plan.Service, err = h.projectServiceByName(project, serviceName)
	if err != nil {
		return plan, err
	}
	plan.Environment, err = h.projectEnvironmentByName(ctx, project, envName)
	if err != nil {
		return plan, err
	}

	envs := h.loadProjectEnvironments(project)
	targetEnv := normalizeEnvironmentName(plan.Environment.Name)
	namespace := h.namespaceForEnvironment(ctx, project, plan.Service, targetEnv, envs)
	after := tunnelRouteServiceURLFor(plan.Service.Name, namespace, h.resolveTunnelBackendPort(ctx, plan.Service, namespace, 0))

	for _, junction := range plan.Junctions {
		current := h.resolveJunctionEnvironment(ctx, junction, envs)
		row := junctionRebindRow{
			JunctionID:         junction.ID.String(),
			Hostname:           junction.Domain,
			Path:               junction.Path,
			CurrentService:     h.serviceNameByID(junction.ServiceID),
			CurrentEnvironment: current.Name,
			CurrentEnvSource:   current.Source,
			TargetService:      plan.Service.Name,
			TargetEnvironment:  targetEnv,
			BackendAfter:       after,
			Action:             "rebind",
		}
		if junction.ServiceID == plan.Service.ID && junction.EnvironmentID != nil && *junction.EnvironmentID == plan.Environment.ID {
			row.Action = "noop"
		}
		if live, known := h.existingTunnelRoute(ctx, junction.Domain); known && live != nil {
			row.LiveBackend = live.Service
			row.LiveMatches = sameBackendURL(live.Service, after)
		}
		plan.Rows = append(plan.Rows, row)
	}
	return plan, nil
}

func (h *Handler) projectServiceByName(project *types.Project, name string) (*types.Service, error) {
	list, err := h.repos.Services.ListByProject(project.ID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	names := []string{}
	for _, service := range list {
		if service == nil {
			continue
		}
		if strings.EqualFold(service.Name, strings.TrimSpace(name)) {
			return service, nil
		}
		names = append(names, service.Name)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("project %s has no service %q (services: %s)", project.Slug, name, strings.Join(names, ", "))
}

// projectEnvironmentByName finds an environment; it creates only the default
// production environment, exactly as every other junction path does. Any
// other missing environment is refused rather than invented.
func (h *Handler) projectEnvironmentByName(ctx context.Context, project *types.Project, name string) (*types.Environment, error) {
	want := normalizeEnvironmentName(name)
	if want == defaultProductionEnvironmentName {
		if env, err := h.ensureDefaultProductionEnvironment(ctx, project); err == nil && env != nil {
			return env, nil
		}
	}
	envs := h.loadProjectEnvironments(project)
	if env, ok := envs.byName[want]; ok {
		return env, nil
	}
	names := make([]string, 0, len(envs.byName))
	for envName := range envs.byName {
		names = append(names, envName)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("project %s has no environment %q (environments: %s)", project.Slug, name, strings.Join(names, ", "))
}

func (h *Handler) serviceNameByID(id uuid.UUID) string {
	service, err := h.repos.Services.GetByID(id)
	if err != nil || service == nil {
		return id.String()
	}
	return service.Name
}

// sameBackendURL compares two rule backends by what they target, not by how
// the URL is spelled.
func sameBackendURL(a, b string) bool {
	if a == b {
		return true
	}
	left, okLeft := parseClusterBackend(a)
	right, okRight := parseClusterBackend(b)
	return okLeft && okRight && left == right
}

func tunnelRouteServiceURLFor(service, namespace string, port int) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", service, namespace, port)
}

func (h *Handler) junctionRebindResponse(operation string, plan junctionRebindPlan, dryRun bool) operatorOperationResponse {
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		DryRun:      dryRun,
		Data: map[string]any{
			"project": plan.Project.Slug, "service": plan.Service.Name,
			"environment": normalizeEnvironmentName(plan.Environment.Name),
			"rows":        plan.Rows, "changes": plan.changes(),
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "admin RBAC; reason required on apply"},
			{Name: "load-state", Status: "completed", Detail: "resolved project, junction(s), target service and environment"},
			{Name: "rebind", Status: "planned", Detail: "rewrite the junction's service and environment; the tunnel route is NOT touched"},
			{Name: "audit", Status: "planned", Detail: "operation and reason recorded by the audit middleware"},
		},
		Warnings: []string{
			"data only: live traffic does not change until tunnels-apply runs, and its repoint guard still applies",
			"the hostname's domain record (custom_domains) is not changed; it does not decide tunnel routing",
		},
	}
}

func rebindInvalid(operation string, dryRun bool, err error) operatorOperationResponse {
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "invalid_request",
		DryRun:      dryRun,
		Summary:     "ops.junctions.rebind refused: " + err.Error(),
		Warnings:    []string{"nothing was changed"},
	}
}

func (h *Handler) handleOpsJunctionsRebindDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	plan, err := h.planJunctionRebind(ctx, req)
	if err != nil {
		return rebindInvalid(operation, true, err)
	}
	resp := h.junctionRebindResponse(operation, plan, true)
	host := plan.Rows[0].Hostname
	env := normalizeEnvironmentName(plan.Environment.Name)
	if plan.changes() == 0 {
		resp.Status = "succeeded"
		resp.Summary = fmt.Sprintf("%s is already bound to %s/%s; nothing to do", host, plan.Service.Name, env)
		resp.Next = []string{fmt.Sprintf("enclii providers cloudflare tunnels-apply %s --project %s", host, plan.Project.Slug)}
		return resp
	}
	resp.Status = "ready_to_apply"
	resp.Summary = fmt.Sprintf("plan to rebind %d junction(s) for %s to %s/%s", plan.changes(), host, plan.Service.Name, env)
	resp.Next = []string{fmt.Sprintf(
		"enclii ops junctions rebind %s --project %s --to-service %s --environment %s --apply --reason \"bind %s to the service and environment that serve it\"",
		host, plan.Project.Slug, plan.Service.Name, env, host)}
	return resp
}

func (h *Handler) handleOpsJunctionsRebindApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	plan, err := h.planJunctionRebind(ctx, req)
	if err != nil {
		return rebindInvalid(operation, false, err), http.StatusBadRequest
	}
	resp := h.junctionRebindResponse(operation, plan, false)
	host := plan.Rows[0].Hostname
	env := normalizeEnvironmentName(plan.Environment.Name)
	resp.Next = []string{fmt.Sprintf("enclii providers cloudflare tunnels-apply %s --project %s", host, plan.Project.Slug)}

	if plan.changes() == 0 {
		resp.Status = "succeeded"
		resp.Summary = fmt.Sprintf("%s is already bound to %s/%s; nothing was changed", host, plan.Service.Name, env)
		return resp, http.StatusOK
	}

	environmentID := plan.Environment.ID
	failed := []string{}
	for i, junction := range plan.Junctions {
		if plan.Rows[i].Action != "rebind" {
			continue
		}
		if err := h.repos.Junctions.Rebind(ctx, junction.ID, plan.Project.ID, plan.Service.ID, &environmentID); err != nil {
			failed = append(failed, fmt.Sprintf("%s%s: %v", junction.Domain, junction.Path, err))
			continue
		}
		h.logger.Info(ctx, "Junction rebound by operator",
			logging.String("domain", junction.Domain),
			logging.String("project", plan.Project.Slug),
			logging.String("from_service", plan.Rows[i].CurrentService),
			logging.String("to_service", plan.Service.Name),
			logging.String("environment", env),
			logging.String("reason", strings.TrimSpace(req.Reason)))
	}
	if len(failed) > 0 {
		resp.Status = "failed"
		resp.Summary = fmt.Sprintf("rebind of %s failed for %d junction(s); re-running is safe", host, len(failed))
		resp.Warnings = append(failed, resp.Warnings...)
		return resp, http.StatusInternalServerError
	}
	resp.Status = "succeeded"
	resp.Summary = fmt.Sprintf("rebound %d junction(s) for %s to %s/%s; the tunnel route was not touched",
		plan.changes(), host, plan.Service.Name, env)
	return resp, http.StatusOK
}
