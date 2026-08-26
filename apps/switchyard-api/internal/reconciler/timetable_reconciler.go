package reconciler

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const (
	// defaultTimetableInterval is the default reconciliation interval.
	defaultTimetableInterval = 30 * time.Second

	// labelManagedBy identifies resources created by the timetable reconciler.
	labelManagedBy      = "app.kubernetes.io/managed-by"
	labelManagedByValue = "enclii"

	// labelCronJobID links a K8s CronJob back to the database record.
	labelCronJobID = "enclii.dev/cron-job-id"

	// LabelOneOffJobID links a K8s Job (and its pods) back to the database
	// record. Exported: the API's one-off job logs endpoint selects pods by it.
	LabelOneOffJobID = "enclii.dev/one-off-job-id"

	// defaultJobImage is the last-resort image used when the job has no
	// explicit image AND the target service's Deployment cannot be resolved
	// (service deleted, never deployed, or K8s lookup failure). Jobs for
	// deployed services run in the service's own image instead -- see
	// resolveJobRuntimeContext.
	defaultJobImage = "busybox:latest"
)

// TimetableReconciler periodically reconciles cron jobs and one-off jobs from the
// database into Kubernetes CronJob and Job resources. It runs as a simple periodic
// loop, independent of the main deployment reconciler work queue.
type TimetableReconciler struct {
	repos     *db.Repositories
	k8sClient *k8s.Client
	logger    *logrus.Logger
	stopCh    chan struct{}
	interval  time.Duration
}

// NewTimetableReconciler creates a new timetable reconciler with a 30-second
// default interval.
func NewTimetableReconciler(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *TimetableReconciler {
	return &TimetableReconciler{
		repos:     repos,
		k8sClient: k8sClient,
		logger:    logger,
		stopCh:    make(chan struct{}),
		interval:  defaultTimetableInterval,
	}
}

// Start begins the timetable reconciliation loop in a goroutine. It runs an
// immediate reconciliation pass on startup, then ticks at the configured interval.
func (r *TimetableReconciler) Start(ctx context.Context) {
	r.logger.WithField("interval", r.interval).Info("Starting timetable reconciler")

	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		// Run an immediate pass on startup.
		r.reconcile(ctx)

		for {
			select {
			case <-ticker.C:
				r.reconcile(ctx)
			case <-r.stopCh:
				r.logger.Info("Timetable reconciler stopped")
				return
			case <-ctx.Done():
				r.logger.Info("Timetable reconciler context cancelled")
				return
			}
		}
	}()
}

// Stop gracefully shuts down the reconciliation loop.
func (r *TimetableReconciler) Stop() {
	close(r.stopCh)
}

// reconcile performs a single reconciliation pass: sync cron jobs, dispatch
// pending one-off jobs, and update running job statuses.
func (r *TimetableReconciler) reconcile(ctx context.Context) {
	r.reconcileCronJobs(ctx)
	r.reconcileOneOffJobs(ctx)
	r.updateRunningOneOffJobs(ctx)
}

// ---------------------------------------------------------------------------
// Cron job reconciliation
// ---------------------------------------------------------------------------

// reconcileCronJobs fetches all active (non-suspended) cron jobs from the DB and
// ensures each has a corresponding Kubernetes CronJob in the correct namespace.
func (r *TimetableReconciler) reconcileCronJobs(ctx context.Context) {
	if r.repos.CronJobs == nil {
		return
	}

	jobs, err := r.repos.CronJobs.ListActive(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Timetable: failed to list active cron jobs")
		return
	}

	if len(jobs) == 0 {
		return
	}

	r.logger.WithField("count", len(jobs)).Debug("Timetable: reconciling active cron jobs")

	for _, job := range jobs {
		if err := r.reconcileCronJob(ctx, job); err != nil {
			r.logger.WithError(err).WithFields(logrus.Fields{
				"cron_job_id": job.ID,
				"name":        job.Name,
			}).Error("Timetable: failed to reconcile cron job")
		}
	}
}

// reconcileCronJob ensures the K8s CronJob for a single database cron job record
// exists and matches the desired state.
func (r *TimetableReconciler) reconcileCronJob(ctx context.Context, job *types.CronJob) error {
	if !r.k8sClient.IsValid() {
		r.logger.WithFields(logrus.Fields{
			"cron_job_id": job.ID,
			"name":        job.Name,
			"schedule":    job.Schedule,
		}).Warn("Timetable: K8s client not available, skipping cron job reconciliation")
		return nil
	}

	// Resolve the target namespace from the project slug.
	namespace, err := r.resolveNamespace(ctx, job.ProjectID)
	if err != nil {
		return fmt.Errorf("resolve namespace for project %s: %w", job.ProjectID, err)
	}

	cronJobName := cronJobK8sName(job)
	rc := r.resolveJobRuntimeContext(ctx, job.ServiceID, namespace, job.Image)
	desired := r.buildCronJob(job, cronJobName, namespace, rc)

	existing, err := r.k8sClient.Clientset.BatchV1().CronJobs(namespace).Get(ctx, cronJobName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("get CronJob %s/%s: %w", namespace, cronJobName, err)
		}

		// CronJob does not exist -- create it.
		if _, createErr := r.k8sClient.Clientset.BatchV1().CronJobs(namespace).Create(ctx, desired, metav1.CreateOptions{}); createErr != nil {
			return fmt.Errorf("create CronJob %s/%s: %w", namespace, cronJobName, createErr)
		}

		r.logger.WithFields(logrus.Fields{
			"cron_job_id": job.ID,
			"name":        cronJobName,
			"namespace":   namespace,
			"schedule":    job.Schedule,
		}).Info("Timetable: created K8s CronJob")
		return nil
	}

	// CronJob exists -- update it if the spec has drifted.
	if r.cronJobNeedsUpdate(existing, job, rc) {
		desired.ResourceVersion = existing.ResourceVersion
		if _, updateErr := r.k8sClient.Clientset.BatchV1().CronJobs(namespace).Update(ctx, desired, metav1.UpdateOptions{}); updateErr != nil {
			return fmt.Errorf("update CronJob %s/%s: %w", namespace, cronJobName, updateErr)
		}

		r.logger.WithFields(logrus.Fields{
			"cron_job_id": job.ID,
			"name":        cronJobName,
			"namespace":   namespace,
		}).Info("Timetable: updated K8s CronJob")
	}

	return nil
}

// buildCronJob constructs the desired K8s CronJob spec from the database record
// and the resolved service runtime context (image, env, serviceAccount,
// imagePullSecrets -- see resolveJobRuntimeContext).
func (r *TimetableReconciler) buildCronJob(job *types.CronJob, name, namespace string, rc jobRuntimeContext) *batchv1.CronJob {
	timeout := int64(job.Timeout)
	if timeout <= 0 {
		timeout = 3600 // 1 hour default
	}

	retries := int32(job.Retries)
	if retries < 0 {
		retries = 0
	}

	labels := map[string]string{
		labelManagedBy: labelManagedByValue,
		labelCronJobID: job.ID.String(),
	}

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:          job.Schedule,
			ConcurrencyPolicy: mapConcurrencyPolicy(job.Concurrency),
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: batchv1.JobSpec{
					ActiveDeadlineSeconds: &timeout,
					BackoffLimit:          &retries,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: labels,
						},
						Spec: corev1.PodSpec{
							RestartPolicy:      corev1.RestartPolicyNever,
							ServiceAccountName: rc.ServiceAccountName,
							ImagePullSecrets:   rc.ImagePullSecrets,
							Containers: []corev1.Container{
								{
									Name:    "job",
									Image:   rc.Image,
									Command: []string{"/bin/sh", "-c", job.Command},
									Env:     rc.Env,
									EnvFrom: rc.EnvFrom,
								},
							},
						},
					},
				},
			},
		},
	}
}

// cronJobNeedsUpdate returns true if the live K8s CronJob diverges from the
// database record on fields the reconciler manages. The image and pod context
// are compared against the RESOLVED runtime context (the same one buildCronJob
// consumes) -- comparing against job.Image/defaultJobImage directly would make
// the reconcile loop fight itself, rewriting the CronJob on every pass.
func (r *TimetableReconciler) cronJobNeedsUpdate(existing *batchv1.CronJob, job *types.CronJob, rc jobRuntimeContext) bool {
	if existing.Spec.Schedule != job.Schedule {
		return true
	}

	if existing.Spec.ConcurrencyPolicy != mapConcurrencyPolicy(job.Concurrency) {
		return true
	}

	// Check command, image and inherited container context.
	podSpec := existing.Spec.JobTemplate.Spec.Template.Spec
	if len(podSpec.Containers) > 0 {
		expectedCmd := []string{"/bin/sh", "-c", job.Command}
		if !stringSliceEqual(podSpec.Containers[0].Command, expectedCmd) {
			return true
		}

		if podSpec.Containers[0].Image != rc.Image {
			return true
		}

		if !typedSlicesEqual(podSpec.Containers[0].Env, rc.Env) {
			return true
		}

		if !typedSlicesEqual(podSpec.Containers[0].EnvFrom, rc.EnvFrom) {
			return true
		}
	}

	// Check inherited pod context.
	if podSpec.ServiceAccountName != rc.ServiceAccountName {
		return true
	}

	if !typedSlicesEqual(podSpec.ImagePullSecrets, rc.ImagePullSecrets) {
		return true
	}

	return false
}

// ---------------------------------------------------------------------------
// One-off job reconciliation
// ---------------------------------------------------------------------------

// reconcileOneOffJobs fetches pending one-off jobs whose run_at time has passed
// (or is nil, meaning "run immediately") and creates K8s Jobs for each.
func (r *TimetableReconciler) reconcileOneOffJobs(ctx context.Context) {
	if r.repos.OneOffJobs == nil {
		return
	}

	jobs, err := r.repos.OneOffJobs.ListPending(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Timetable: failed to list pending one-off jobs")
		return
	}

	if len(jobs) == 0 {
		return
	}

	r.logger.WithField("count", len(jobs)).Debug("Timetable: dispatching pending one-off jobs")

	for _, job := range jobs {
		if err := r.dispatchOneOffJob(ctx, job); err != nil {
			r.logger.WithError(err).WithFields(logrus.Fields{
				"one_off_job_id": job.ID,
				"name":           job.Name,
			}).Error("Timetable: failed to dispatch one-off job")
		}
	}
}

// dispatchOneOffJob creates a K8s Job for a pending one-off job and marks it
// as running in the database.
func (r *TimetableReconciler) dispatchOneOffJob(ctx context.Context, job *types.OneOffJob) error {
	if !r.k8sClient.IsValid() {
		r.logger.WithFields(logrus.Fields{
			"one_off_job_id": job.ID,
			"name":           job.Name,
		}).Warn("Timetable: K8s client not available, skipping one-off job dispatch")
		return nil
	}

	namespace, err := r.resolveNamespace(ctx, job.ProjectID)
	if err != nil {
		return fmt.Errorf("resolve namespace for project %s: %w", job.ProjectID, err)
	}

	rc := r.resolveJobRuntimeContext(ctx, job.ServiceID, namespace, job.Image)
	k8sJob := r.buildOneOffJob(job, namespace, rc)

	_, err = r.k8sClient.Clientset.BatchV1().Jobs(namespace).Create(ctx, k8sJob, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Job was already created in a prior reconciliation pass.
			r.logger.WithFields(logrus.Fields{
				"one_off_job_id": job.ID,
				"name":           k8sJob.Name,
				"namespace":      namespace,
			}).Debug("Timetable: K8s Job already exists, updating status to running")
		} else {
			return fmt.Errorf("create Job %s/%s: %w", namespace, k8sJob.Name, err)
		}
	} else {
		r.logger.WithFields(logrus.Fields{
			"one_off_job_id": job.ID,
			"name":           k8sJob.Name,
			"namespace":      namespace,
		}).Info("Timetable: created K8s Job for one-off execution")
	}

	// Mark the job as running in the database.
	if updateErr := r.repos.OneOffJobs.UpdateStatus(ctx, job.ID, "running", nil); updateErr != nil {
		r.logger.WithError(updateErr).WithField("one_off_job_id", job.ID).Error("Timetable: failed to mark one-off job as running")
	}

	return nil
}

// buildOneOffJob constructs the desired K8s Job spec from the database record
// and the resolved service runtime context (image, env, serviceAccount,
// imagePullSecrets -- see resolveJobRuntimeContext).
func (r *TimetableReconciler) buildOneOffJob(job *types.OneOffJob, namespace string, rc jobRuntimeContext) *batchv1.Job {
	timeout := int64(job.Timeout)
	if timeout <= 0 {
		timeout = 3600
	}

	// One-off jobs do not retry by default.
	var backoffLimit int32

	labels := map[string]string{
		labelManagedBy:   labelManagedByValue,
		LabelOneOffJobID: job.ID.String(),
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OneOffJobK8sName(job),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			ActiveDeadlineSeconds: &timeout,
			BackoffLimit:          &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: rc.ServiceAccountName,
					ImagePullSecrets:   rc.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:    "job",
							Image:   rc.Image,
							Command: []string{"/bin/sh", "-c", job.Command},
							Env:     rc.Env,
							EnvFrom: rc.EnvFrom,
						},
					},
				},
			},
		},
	}
}

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
	fallback := jobRuntimeContext{Image: explicitImage}
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

	deployment, err := r.k8sClient.Kube().AppsV1().Deployments(namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		r.logger.WithError(err).WithFields(logrus.Fields{
			"service":   svc.Name,
			"namespace": namespace,
			"fallback":  fallback.Image,
		}).Warn("Timetable: could not read service Deployment for job runtime context, falling back to default image without service env")
		return fallback
	}

	return runtimeContextFromDeployment(deployment, explicitImage)
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
	}

	if len(podSpec.Containers) > 0 {
		first := podSpec.Containers[0]
		if rc.Image == "" {
			rc.Image = first.Image
		}
		rc.Env = first.Env
		rc.EnvFrom = first.EnvFrom
	}

	if rc.Image == "" {
		rc.Image = defaultJobImage
	}

	return rc
}

// ---------------------------------------------------------------------------
// Status synchronization (K8s -> DB)
// ---------------------------------------------------------------------------

// updateRunningOneOffJobs checks K8s Job completion status across all project
// namespaces and updates the corresponding one-off job records in the database.
func (r *TimetableReconciler) updateRunningOneOffJobs(ctx context.Context) {
	if r.repos.OneOffJobs == nil {
		return
	}

	if !r.k8sClient.IsValid() {
		return
	}

	projects, err := r.repos.Projects.List()
	if err != nil {
		r.logger.WithError(err).Error("Timetable: failed to list projects for status sync")
		return
	}

	for _, project := range projects {
		if project.Slug == "" {
			continue
		}
		r.syncJobStatusInNamespace(ctx, project.Slug)
	}
}

// syncJobStatusInNamespace lists completed/failed K8s Jobs in a namespace and
// updates the corresponding one-off job records in the database.
func (r *TimetableReconciler) syncJobStatusInNamespace(ctx context.Context, namespace string) {
	jobList, err := r.k8sClient.Clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", labelManagedBy, labelManagedByValue),
	})
	if err != nil {
		// Namespace may not exist yet -- not an error condition.
		if errors.IsNotFound(err) {
			return
		}
		r.logger.WithError(err).WithField("namespace", namespace).Debug("Timetable: failed to list jobs in namespace")
		return
	}

	for i := range jobList.Items {
		k8sJob := &jobList.Items[i]

		jobIDStr, ok := k8sJob.Labels[LabelOneOffJobID]
		if !ok {
			// Not a one-off job (could be a CronJob-spawned Job). Skip.
			continue
		}

		// Determine completion status from K8s Job conditions.
		var newStatus string
		var exitCode *int

		for _, cond := range k8sJob.Status.Conditions {
			switch cond.Type {
			case batchv1.JobComplete:
				if cond.Status == corev1.ConditionTrue {
					newStatus = "completed"
					code := 0
					exitCode = &code
				}
			case batchv1.JobFailed:
				if cond.Status == corev1.ConditionTrue {
					newStatus = "failed"
					code := 1
					exitCode = &code
				}
			}
		}

		if newStatus == "" {
			// Job still running -- no status update needed.
			continue
		}

		jobID, parseErr := uuid.Parse(jobIDStr)
		if parseErr != nil {
			r.logger.WithField("label_value", jobIDStr).Warn("Timetable: invalid UUID in one-off job label")
			continue
		}

		if updateErr := r.repos.OneOffJobs.UpdateStatus(ctx, jobID, newStatus, exitCode); updateErr != nil {
			r.logger.WithError(updateErr).WithFields(logrus.Fields{
				"one_off_job_id": jobIDStr,
				"status":         newStatus,
			}).Warn("Timetable: failed to update one-off job status from K8s")
		} else {
			r.logger.WithFields(logrus.Fields{
				"one_off_job_id": jobIDStr,
				"status":         newStatus,
				"namespace":      namespace,
			}).Info("Timetable: synced one-off job status from K8s")
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveNamespace returns the K8s namespace for a project. The convention is
// to use the project slug as the namespace, matching the service reconciler.
func (r *TimetableReconciler) resolveNamespace(ctx context.Context, projectID uuid.UUID) (string, error) {
	project, err := r.repos.Projects.GetByID(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("get project %s: %w", projectID, err)
	}

	if project.Slug == "" {
		return "", fmt.Errorf("project %s has empty slug", projectID)
	}

	return project.Slug, nil
}

// cronJobK8sName derives a deterministic K8s CronJob name from the database
// record. Format: "cj-<sanitized-name>", truncated to 52 characters. K8s
// CronJob names must be <= 52 chars because spawned Job names append a suffix.
func cronJobK8sName(job *types.CronJob) string {
	name := fmt.Sprintf("cj-%s", sanitizeK8sName(job.Name))
	if len(name) > 52 {
		name = name[:52]
	}
	return name
}

// OneOffJobK8sName derives a deterministic K8s Job name from the database record.
// Includes a truncated UUID suffix for uniqueness across re-runs of the same
// named job.
func OneOffJobK8sName(job *types.OneOffJob) string {
	idSuffix := job.ID.String()[:8]
	name := fmt.Sprintf("job-%s-%s", sanitizeK8sName(job.Name), idSuffix)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// sanitizeK8sName converts a string to a valid Kubernetes resource name
// component: lowercase, alphanumeric and dashes only, trimmed of leading
// and trailing dashes.
func sanitizeK8sName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			b.WriteRune(ch)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// mapConcurrencyPolicy maps a database concurrency string to the K8s
// ConcurrencyPolicy enum. Defaults to AllowConcurrent for unrecognised values.
func mapConcurrencyPolicy(policy string) batchv1.ConcurrencyPolicy {
	switch strings.ToLower(policy) {
	case "forbid":
		return batchv1.ForbidConcurrent
	case "replace":
		return batchv1.ReplaceConcurrent
	default:
		return batchv1.AllowConcurrent
	}
}

// stringSliceEqual returns true if two string slices are identical in length
// and content.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// typedSlicesEqual returns true if two slices are identical in length and
// content, treating nil and empty as equal (reflect.DeepEqual alone would not,
// which would make the reconcile loop rewrite specs where the API server
// returns nil for a field we submitted as absent).
func typedSlicesEqual[T any](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
