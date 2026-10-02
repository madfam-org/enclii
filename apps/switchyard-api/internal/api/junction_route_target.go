package api

// Where a junction's traffic should go: the environment whose workload serves
// the hostname, and that environment's namespace.
//
// A junction binds a hostname to a service. Until migration 041 it carried no
// environment at all, and every reconciler that reads junctions derived the
// backend as `<service>.<production namespace>.svc`. A staging hostname could
// therefore never be planned correctly: on a project with a staging
// environment, `tunnels-apply` proposed moving every live staging route onto
// the PRODUCTION workload, and applied it when asked.
//
// The environment is now resolved per junction, from the most explicit record
// to the least:
//
//  1. the environment recorded on the junction itself (written by an audited
//     rebind, or by the provisioner that knows which environment it is
//     provisioning);
//  2. the environment on the hostname's custom_domains row, which the manifest
//     provisioner has always recorded from `spec.domains[].environment`;
//  3. production, which is what every junction meant before either existed.
//
// The namespace is then the environment's own: the namespace recorded on the
// environment row, else the `enclii-<project>-<env>` convention every
// non-production deploy uses. A non-production environment NEVER falls back to
// the service's own namespace, because for an adopted service that namespace
// is the production one.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Where a junction's environment came from. Reported on every plan row so an
// operator can see WHY a hostname is planned against an environment, and
// which record to correct when it is the wrong one.
const (
	junctionEnvFromJunction     = "junction"
	junctionEnvFromDomainRecord = "domain-record"
	junctionEnvDefault          = "default"
)

// projectEnvironments indexes one project's environments the three ways the
// route planner asks about them.
type projectEnvironments struct {
	byID   map[uuid.UUID]*types.Environment
	byName map[string]*types.Environment
	// namespaceEnv maps a Kubernetes namespace to the environment that runs
	// in it. This is what lets a live route's backend be classified: a rule
	// pointing into `enclii-<project>-staging` is a staging route whatever the
	// junction says.
	namespaceEnv map[string]string
}

// environmentOfNamespace names the environment a namespace belongs to, or ""
// when the namespace is not one of this project's (an adopted workload in a
// namespace of its own, say).
func (e projectEnvironments) environmentOfNamespace(namespace string) string {
	return e.namespaceEnv[strings.TrimSpace(namespace)]
}

// loadProjectEnvironments reads a project's environments once per plan.
//
// The production namespace is always classified, even when the environments
// table has no production row yet: a rule pointing into the project namespace
// is a production rule, and a guard that cannot see that is no guard.
func (h *Handler) loadProjectEnvironments(project *types.Project) projectEnvironments {
	envs := projectEnvironments{
		byID:         map[uuid.UUID]*types.Environment{},
		byName:       map[string]*types.Environment{},
		namespaceEnv: map[string]string{},
	}
	if project == nil {
		return envs
	}
	if prod := defaultProductionNamespace(project); prod != "" {
		envs.namespaceEnv[prod] = defaultProductionEnvironmentName
	}
	if h == nil || h.repos == nil || h.repos.Environments == nil {
		return envs
	}
	list, err := h.repos.Environments.ListByProject(project.ID)
	if err != nil {
		return envs
	}
	for _, env := range list {
		if env == nil {
			continue
		}
		envs.byID[env.ID] = env
		envs.byName[normalizeEnvironmentName(env.Name)] = env
		if ns := strings.TrimSpace(env.KubeNamespace); ns != "" {
			envs.namespaceEnv[ns] = normalizeEnvironmentName(env.Name)
		}
	}
	return envs
}

// normalizeEnvironmentName folds the spellings of one environment together.
// `prod` and `production` are the same environment everywhere else in the
// codebase (isProductionEnvironmentName), so they must be here too.
func normalizeEnvironmentName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if isProductionEnvironmentName(name) {
		return defaultProductionEnvironmentName
	}
	return name
}

// junctionEnvironment is the resolved environment of one junction.
type junctionEnvironment struct {
	Name   string
	Source string
	// Note explains a fallback the operator should know about: a recorded
	// environment that no longer exists, or domain records that disagree.
	Note string
}

// resolveJunctionEnvironment decides which environment serves a junction's
// hostname. See the file comment for the precedence and why.
func (h *Handler) resolveJunctionEnvironment(
	ctx context.Context, junction *types.Junction, envs projectEnvironments,
) junctionEnvironment {
	note := ""
	if junction != nil && junction.EnvironmentID != nil {
		if env, ok := envs.byID[*junction.EnvironmentID]; ok {
			return junctionEnvironment{Name: normalizeEnvironmentName(env.Name), Source: junctionEnvFromJunction}
		}
		note = "the environment recorded on this junction is not one of the project's environments; ignored"
	}

	if fromRecord, recordNote := h.environmentFromDomainRecords(ctx, junction, envs); fromRecord != "" {
		return junctionEnvironment{Name: fromRecord, Source: junctionEnvFromDomainRecord, Note: joinNotes(note, recordNote)}
	} else if recordNote != "" {
		note = joinNotes(note, recordNote)
	}

	return junctionEnvironment{Name: defaultProductionEnvironmentName, Source: junctionEnvDefault, Note: note}
}

// environmentFromDomainRecords reads the environment the hostname's
// custom_domains rows were registered under.
//
// Only rows whose environment belongs to THIS project count: a row another
// project holds says nothing about where this project's workload runs. When
// the rows name more than one environment, the row on the junction's own
// service wins; if that still does not settle it, nothing is guessed and the
// disagreement is reported.
func (h *Handler) environmentFromDomainRecords(
	ctx context.Context, junction *types.Junction, envs projectEnvironments,
) (string, string) {
	if h == nil || h.repos == nil || h.repos.CustomDomains == nil || junction == nil || junction.Domain == "" {
		return "", ""
	}
	rows, err := h.repos.CustomDomains.ListByDomain(ctx, junction.Domain)
	if err != nil {
		return "", fmt.Sprintf("could not read the domain record's environment: %v", err)
	}

	names := map[string]struct{}{}
	sameService := map[string]struct{}{}
	for _, row := range rows {
		env, ok := envs.byID[row.EnvironmentID]
		if !ok {
			continue
		}
		name := normalizeEnvironmentName(env.Name)
		names[name] = struct{}{}
		if row.ServiceID == junction.ServiceID {
			sameService[name] = struct{}{}
		}
	}

	switch {
	case len(names) == 1:
		return onlyKey(names), ""
	case len(names) > 1 && len(sameService) == 1:
		return onlyKey(sameService), ""
	case len(names) > 1:
		return "", fmt.Sprintf("the domain records name several environments (%s); none was chosen",
			strings.Join(sortedEnvironmentNames(names), ", "))
	}
	return "", ""
}

// namespaceForEnvironment is the namespace an environment's workload runs in.
//
// Production keeps the long-standing resolution (the service's recorded
// namespace first, because adopted workloads can live outside the project
// namespace). Every other environment uses its own namespace and nothing
// else.
func (h *Handler) namespaceForEnvironment(
	ctx context.Context, project *types.Project, service *types.Service, envName string, envs projectEnvironments,
) string {
	if isProductionEnvironmentName(envName) {
		return h.resolveServiceNamespace(ctx, service, defaultProductionEnvironmentName)
	}
	if env, ok := envs.byName[normalizeEnvironmentName(envName)]; ok && strings.TrimSpace(env.KubeNamespace) != "" {
		return strings.TrimSpace(env.KubeNamespace)
	}
	if project == nil || strings.TrimSpace(project.Slug) == "" {
		return ""
	}
	return legacyAutoDeployKubeNamespace(project, envName)
}

func onlyKey(set map[string]struct{}) string {
	for key := range set {
		return key
	}
	return ""
}

func sortedEnvironmentNames(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinNotes(notes ...string) string {
	kept := make([]string, 0, len(notes))
	for _, note := range notes {
		if strings.TrimSpace(note) != "" {
			kept = append(kept, note)
		}
	}
	return strings.Join(kept, "; ")
}
