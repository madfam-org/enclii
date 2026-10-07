package reconciler

import (
	"context"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---------------------------------------------------------------------------
// Service runtime context resolution
// ---------------------------------------------------------------------------

// jobRuntimeContext carries the execution context a timetable job pod runs
// with: the container image plus the env, envFrom, serviceAccount and
// imagePullSecrets inherited from the target service's live Deployment. It is
// resolved once per reconcile of a job and passed to the builders so that the
// desired spec and the drift comparison always derive from the same source.
type jobRuntimeContext struct {
	Image              string
	Env                []corev1.EnvVar
	EnvFrom            []corev1.EnvFromSource
	ServiceAccountName string
	ImagePullSecrets   []corev1.LocalObjectReference
	// Security contexts are inherited from the service Deployment so job pods
	// pass the same admission policies (Kyverno restrict-capabilities /
	// require-run-as-non-root) that the service itself passes. A CronJob whose
	// pods declared no securityContext has been DENIED admission in this
	// cluster before (karafiel #210) — without this, jobs would dispatch and
	// then sit podless.
	PodSecurityContext       *corev1.PodSecurityContext
	ContainerSecurityContext *corev1.SecurityContext
}

// resolveJobRuntimeContext derives the execution context for a timetable job
// from its target service's live Deployment (Deployment name == service name,
// the convention used by the service reconciler and the topology builder).
// The command `rails db:migrate` only works when the job pod runs the same
// image with the same env/secrets as the service it belongs to.
//
// Resolution order:
//  1. Load the service record for job.ServiceID, then read the Deployment
//     named after the service in the project namespace.
//  2. Copy image/env/envFrom from the Deployment's first container and
//     serviceAccountName/imagePullSecrets from its pod spec.
//  3. explicitImage (job.Image set by the user) overrides the service image
//     but the job STILL inherits env/serviceAccount/pullSecrets when the
//     Deployment is resolvable: the common case for an image override is
//     running a sibling tool (e.g. migrate/migrate) against the same
//     configuration the service runs with.
//  4. If the service or its Deployment cannot be resolved, fall back to
//     explicitImage or defaultJobImage with no inherited context, preserving
//     the pre-existing behavior for services that were never deployed. The
//     reason is logged as a warning so operators can tell why a job ran in
//     busybox.
//
// SECURITY: inheriting the service's env (including secret references),
// service account and image pull secrets into a job grants the job the same
// privilege as deploying code to the service itself. That is intentional:
// cron/one-off job creation is already gated at RequireRole(Developer) on the
// API -- the same role required to deploy the service -- so this resolution
// adds no privilege beyond what the job creator already holds.
func (r *TimetableReconciler) resolveJobRuntimeContext(ctx context.Context, serviceID uuid.UUID, namespace, explicitImage string) jobRuntimeContext {
	// Every fallback path is hardened: a job that inherits no securityContext
	// from a Deployment must still supply one of its own or the cluster's
	// Kyverno policies deny its admission.
	fallback := hardenRuntimeContext(jobRuntimeContext{Image: explicitImage})
	if fallback.Image == "" {
		fallback.Image = defaultJobImage
	}

	if r.repos == nil || r.repos.Services == nil {
		r.logger.WithFields(logrus.Fields{
			"service_id": serviceID,
			"namespace":  namespace,
		}).Warn("Timetable: services repository unavailable, job will run without service context")
		return fallback
	}

	svc, err := r.repos.Services.GetByID(serviceID)
	if err != nil || svc == nil {
		r.logger.WithError(err).WithFields(logrus.Fields{
			"service_id": serviceID,
			"namespace":  namespace,
			"fallback":   fallback.Image,
		}).Warn("Timetable: could not load service for job runtime context, falling back to default image without service env")
		return fallback
	}

	if r.k8sClient == nil || r.k8sClient.Kube() == nil {
		r.logger.WithFields(logrus.Fields{
			"service":   svc.Name,
			"namespace": namespace,
		}).Warn("Timetable: K8s client unavailable, job will run without service context")
		return fallback
	}

	deployment, resolvedName, err := r.getServiceDeployment(ctx, namespace, svc.Name)
	if err != nil {
		r.logger.WithError(err).WithFields(logrus.Fields{
			"service":         svc.Name,
			"namespace":       namespace,
			"names_attempted": deploymentNameCandidates(namespace, svc.Name),
			"fallback":        fallback.Image,
		}).Warn("Timetable: could not read service Deployment for job runtime context, falling back to default image with hardened security context")
		return fallback
	}

	r.logger.WithFields(logrus.Fields{
		"service":    svc.Name,
		"namespace":  namespace,
		"deployment": resolvedName,
	}).Debug("Timetable: resolved service Deployment for job runtime context")

	return runtimeContextFromDeployment(deployment, explicitImage)
}

// getServiceDeployment reads the Deployment backing a service, returning the
// name it actually resolved under.
//
// Historically this did a single Get on the bare service name. The fleet's
// service reconciler names Deployments per process type -- `<service>-web`,
// `<service>-worker` -- so the bare name misses for every real service
// (nauta-web, crea-map-web, tezca-web...). That miss silently degraded every
// job to the context-free fallback, which Kyverno then denied.
//
// Three deterministic Gets, in order:
//
//  1. `<serviceName>` exactly -- so any service that IS deployed under its
//     bare name keeps working.
//  2. `<serviceName>-web` -- the per-process-type convention: nauta ->
//     nauta-web, crea-map -> crea-map-web.
//  3. `<namespace>-web` -- the registered service name does not always prefix
//     its deployments. Namespace tezca runs tezca-web/tezca-worker/tezca-redis
//     while the registered service is `tezca-api`, so try 2 would look for
//     `tezca-api-web` and miss. The namespace equals the project slug, which
//     is the stable name the deployments actually carry.
//
// Gets only -- no list/label machinery.
func (r *TimetableReconciler) getServiceDeployment(ctx context.Context, namespace, serviceName string) (*appsv1.Deployment, string, error) {
	deployments := r.k8sClient.Kube().AppsV1().Deployments(namespace)

	candidates := deploymentNameCandidates(namespace, serviceName)

	var lastErr error
	for _, name := range candidates {
		deployment, err := deployments.Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			return deployment, name, nil
		}
		if !errors.IsNotFound(err) {
			// A real API failure (RBAC, connectivity): stop rather than
			// masking it behind a subsequent NotFound.
			return nil, "", err
		}
		lastErr = err
	}

	return nil, "", lastErr
}

// deploymentNameCandidates lists the Deployment names to try for a service, in
// order, de-duplicated so an already-covered name is not fetched twice (e.g. a
// service named exactly after its namespace, where `<service>-web` and
// `<namespace>-web` coincide).
func deploymentNameCandidates(namespace, serviceName string) []string {
	ordered := []string{
		serviceName,
		serviceName + webDeploymentSuffix,
		namespace + webDeploymentSuffix,
	}

	candidates := make([]string, 0, len(ordered))
	seen := make(map[string]bool, len(ordered))
	for _, name := range ordered {
		if name == "" || name == webDeploymentSuffix || seen[name] {
			continue
		}
		seen[name] = true
		candidates = append(candidates, name)
	}

	return candidates
}

// hardenRuntimeContext fills in securityContexts that satisfy the cluster's
// Kyverno baseline for any context that carries none.
//
// A context resolved from a live Deployment already carries the service's own
// securityContext pair (which passes admission, or the service would not be
// running) and is returned untouched. Only the context-free paths -- no
// Deployment found, or an explicit --image against a service that was never
// deployed -- get these defaults. Without them the Job is built with a nil
// securityContext and `restrict-capabilities` (autogen-drop-all-capabilities)
// denies the Create.
func hardenRuntimeContext(rc jobRuntimeContext) jobRuntimeContext {
	if rc.ContainerSecurityContext == nil {
		allowPrivilegeEscalation := false
		runAsNonRoot := true
		runAsUser := hardenedJobRunAsUser
		rc.ContainerSecurityContext = &corev1.SecurityContext{
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			RunAsNonRoot:             &runAsNonRoot,
			RunAsUser:                &runAsUser,
		}
	}

	if rc.PodSecurityContext == nil {
		runAsNonRoot := true
		rc.PodSecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot:   &runAsNonRoot,
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}
	}

	return rc
}

// runtimeContextFromDeployment copies the runtime context out of a service
// Deployment's pod template: image, env and envFrom from the first container,
// plus serviceAccountName and imagePullSecrets from the pod spec. An explicit
// image overrides the container image; defaultJobImage covers the degenerate
// case of a Deployment with no containers.
func runtimeContextFromDeployment(deployment *appsv1.Deployment, explicitImage string) jobRuntimeContext {
	podSpec := deployment.Spec.Template.Spec

	rc := jobRuntimeContext{
		Image:              explicitImage,
		ServiceAccountName: podSpec.ServiceAccountName,
		ImagePullSecrets:   podSpec.ImagePullSecrets,
		PodSecurityContext: podSpec.SecurityContext,
	}

	if len(podSpec.Containers) > 0 {
		first := podSpec.Containers[0]
		if rc.Image == "" {
			rc.Image = first.Image
		}
		rc.Env = first.Env
		rc.EnvFrom = first.EnvFrom
		rc.ContainerSecurityContext = first.SecurityContext
	}

	if rc.Image == "" {
		rc.Image = defaultJobImage
	}

	// A Deployment with no containers (or one that declares no securityContext)
	// yields a context-free job spec, which Kyverno denies. Harden whatever the
	// Deployment did not supply; a Deployment that carries its own pair is
	// returned verbatim.
	return hardenRuntimeContext(rc)
}
