package api

import (
	"context"
	"fmt"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// manifestWebSuffix mirrors reconciler.webDeploymentSuffix. The reconciler's
// constant is unexported and importing internal/reconciler from internal/api
// would drag the whole reconcile loop into the request path, so the two-line
// convention is restated here. Keep in sync with
// internal/reconciler/timetable_reconciler.go.
const manifestWebSuffix = "-web"

// manifestWorkloadCheck is the outcome of validating one parsed manifest doc
// against the live workloads in its target namespace.
type manifestWorkloadCheck struct {
	// Resolved is true when the doc's metadata.name maps to a live workload.
	Resolved bool
	// ResolvedAs names the object that satisfied the lookup ("deployment/janua-api"),
	// empty when nothing resolved.
	ResolvedAs string
	// Inconclusive is true when the k8s API could not answer (RBAC,
	// connectivity, no client configured). Capture proceeds in this case.
	Inconclusive bool
	// Candidates lists the names that were tried, for the operator message.
	Candidates []string
	// Err carries the transient API error behind Inconclusive.
	Err error
}

// manifestWorkloadCandidates lists the workload names to try for a manifest
// doc's metadata.name, in order, de-duplicated.
//
// This is the #463 three-name precedent, mirrored from
// internal/reconciler/timetable_reconciler.go's deploymentNameCandidates:
//
//  1. `<name>` exactly — a service deployed under its bare name.
//  2. `<name>-web` — the per-process-type convention (nauta -> nauta-web).
//  3. `<namespace>-web` — the registered name does not always prefix its
//     deployments (service `tezca-api` in namespace `tezca` runs `tezca-web`).
//
// The reconciler's copy is unexported; see the note on manifestWebSuffix.
func manifestWorkloadCandidates(namespace, name string) []string {
	ordered := []string{
		name,
		name + manifestWebSuffix,
		namespace + manifestWebSuffix,
	}

	candidates := make([]string, 0, len(ordered))
	seen := make(map[string]bool, len(ordered))
	for _, candidate := range ordered {
		if candidate == "" || candidate == manifestWebSuffix || seen[candidate] {
			continue
		}
		seen[candidate] = true
		candidates = append(candidates, candidate)
	}

	return candidates
}

// checkManifestWorkloadResolves answers whether a manifest doc's
// metadata.name corresponds to anything a tunnel route could actually dial in
// the target namespace.
//
// Two object kinds count as resolution:
//
//   - a Deployment under any of the three candidate names, and
//   - a k8s Service named exactly `<name>` — a Service is what tunnel routes
//     dial, so a Service-only setup (no Deployment we can see, e.g. an
//     ExternalName or a workload owned by a CRD) must not be flagged.
//
// Transient (non-NotFound) API errors return Inconclusive rather than a
// failed resolution. The asymmetry with the write-time guard in
// domain_tunnel_backend.go (#467) is deliberate and load-bearing: there,
// resolveTunnelBackend answers a transient failure with errTunnelBackendUnknown,
// which BLOCKS an overwrite of a live tunnel rule, because the
// downside of guessing wrong is rewriting working identity hostnames to a
// dead backend. Here, "couldn't check" must NOT block onboarding, because the
// downside of guessing wrong is refusing to provision a domain that is
// perfectly fine — a self-inflicted outage of the thing we are trying to set
// up. Same question, opposite default, because the failure modes are opposite.
func (h *Handler) checkManifestWorkloadResolves(ctx context.Context, namespace, name string) manifestWorkloadCheck {
	result := manifestWorkloadCheck{Candidates: manifestWorkloadCandidates(namespace, name)}

	if h.k8sClient == nil || h.k8sClient.Kube() == nil {
		result.Inconclusive = true
		result.Err = fmt.Errorf("k8s client not available")
		return result
	}

	kube := h.k8sClient.Kube()

	deployments := kube.AppsV1().Deployments(namespace)
	for _, candidate := range result.Candidates {
		_, err := deployments.Get(ctx, candidate, metav1.GetOptions{})
		if err == nil {
			result.Resolved = true
			result.ResolvedAs = "deployment/" + candidate
			return result
		}
		if !k8serrors.IsNotFound(err) {
			result.Inconclusive = true
			result.Err = err
			return result
		}
	}

	// A Service named exactly for the doc is what a tunnel route dials, so it
	// resolves on its own. Only the exact name: `<name>-web` Services back the
	// `<name>-web` Deployment we already tried, and a namespace-wide guess
	// would resolve names the manifest never declared.
	_, err := kube.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		result.Resolved = true
		result.ResolvedAs = "service/" + name
		return result
	}
	if !k8serrors.IsNotFound(err) {
		result.Inconclusive = true
		result.Err = err
		return result
	}

	return result
}

// guardManifestDomainCapture decides whether domains declared by a parsed
// manifest may be provisioned, and records a loud step failure when they may
// not.
//
// Motivation (2026-08-27 janua outage): janua/enclii.yaml was a legacy single
// Service doc with `metadata.name: janua` declaring eight identity hostnames.
// No cluster Service named `janua` has ever existed — the real ones are
// janua-api / janua-admin / janua-dashboard / janua-website. Domain capture
// happily created records deriving tunnel backends from that name, and every
// identity hostname in the fleet was rewritten to a backend that does not
// exist. This is the capture-time layer: catch the dead name BEFORE any domain
// record is created, so the write-time guards never have to.
//
// Returns true when capture may proceed.
func (h *Handler) guardManifestDomainCapture(
	ctx context.Context,
	steps *[]stepResult,
	namespace string,
	service *types.Service,
	config *manifest.EncliiYAML,
) bool {
	if config == nil || len(config.Spec.Domains) == 0 {
		return true
	}
	docName := config.Metadata.Name
	if docName == "" {
		// Nothing to resolve against; the pre-existing behaviour (provision
		// against the project's first service) is unchanged.
		return true
	}

	check := h.checkManifestWorkloadResolves(ctx, namespace, docName)

	if check.Inconclusive {
		// Never let "couldn't check" block onboarding — see the asymmetry note
		// on checkManifestWorkloadResolves.
		h.logger.Warn(ctx, "Could not validate manifest service name against live workloads; provisioning domains anyway",
			logging.String("manifest_service", docName),
			logging.String("namespace", namespace),
			logging.String("names_attempted", strings.Join(check.Candidates, ", ")),
			logging.Error("error", check.Err))
		return true
	}

	if check.Resolved {
		h.logger.Debug(ctx, "Manifest service name resolves to a live workload",
			logging.String("manifest_service", docName),
			logging.String("namespace", namespace),
			logging.String("resolved_as", check.ResolvedAs))
		return true
	}

	declared := make([]string, 0, len(config.Spec.Domains))
	for _, d := range config.Spec.Domains {
		declared = append(declared, d.Name)
	}

	staleNote := ""
	if n := h.countExistingDomainRecords(ctx, service, declared); n > 0 {
		// Existing records are NOT deleted here: which of them is stale and
		// which is a working domain someone repointed by hand is an operator
		// call, not a capture-time one.
		staleNote = fmt.Sprintf(" %d domain record(s) for these hostnames already exist and were LEFT IN PLACE — review and clean them up manually if they point at the dead name.", n)
	}

	h.recordStep(ctx, steps, "domain_provisioning", false, fmt.Errorf(
		"enclii.yaml declares metadata.name %q, which resolves to NO live workload in namespace %q (tried Deployments %s, and Service %q) — REFUSING to provision the %d domain(s) it declares (%s) because their tunnel backends would be derived from a name nothing serves. This is the janua shape: a legacy single-doc manifest whose name predates the real per-process workloads. Fix the manifest to name a workload that exists (or split it into one doc per deployed service), then re-run onboarding.%s",
		docName,
		namespace,
		strings.Join(check.Candidates, ", "),
		docName,
		len(declared),
		strings.Join(declared, ", "),
		staleNote,
	))

	return false
}

// countExistingDomainRecords counts how many of the declared hostnames already
// have a custom-domain record on this service. Best-effort: a lookup failure
// reports zero rather than derailing the warning that matters.
func (h *Handler) countExistingDomainRecords(ctx context.Context, service *types.Service, declared []string) int {
	if service == nil || h.repos == nil || h.repos.CustomDomains == nil {
		return 0
	}

	existing, err := h.repos.CustomDomains.GetByServiceID(ctx, service.ID.String())
	if err != nil {
		h.logger.Warn(ctx, "Could not count existing domain records while refusing manifest capture",
			logging.String("service", service.Name),
			logging.Error("error", err))
		return 0
	}

	wanted := make(map[string]bool, len(declared))
	for _, name := range declared {
		wanted[canonicalDomain(name)] = true
	}

	count := 0
	for _, record := range existing {
		if wanted[canonicalDomain(record.Domain)] {
			count++
		}
	}
	return count
}
