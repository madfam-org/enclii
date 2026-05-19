package api

import (
	"context"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const (
	projectCardJobFailureWindow    = 24 * time.Hour
	projectCardJobActiveStaleAfter = 30 * time.Minute
	projectCardCronJobNoRunGrace   = 8 * 24 * time.Hour
)

func (h *Handler) listProjectCardJobEvidence(ctx context.Context, observedAt time.Time) map[string]projectCardJobsEvidence {
	client := h.projectCardKubeClient()
	if client == nil {
		return nil
	}

	cronJobs, err := client.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	jobs, err := client.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	cronJobsByNamespace := map[string][]batchv1.CronJob{}
	for _, cronJob := range cronJobs.Items {
		cronJobsByNamespace[cronJob.Namespace] = append(cronJobsByNamespace[cronJob.Namespace], cronJob)
	}
	jobsByNamespace := map[string][]batchv1.Job{}
	for _, job := range jobs.Items {
		jobsByNamespace[job.Namespace] = append(jobsByNamespace[job.Namespace], job)
	}

	evidenceByNamespace := make(map[string]projectCardJobsEvidence, len(cronJobsByNamespace))
	for namespace, namespaceCronJobs := range cronJobsByNamespace {
		evidence := summarizeProjectCardCronJobs(namespaceCronJobs, jobsByNamespace[namespace], observedAt)
		if evidence.CronJobCount > 0 {
			evidenceByNamespace[namespace] = evidence
		}
	}
	return evidenceByNamespace
}

func (h *Handler) projectCardKubeClient() kubernetes.Interface {
	if h == nil || h.k8sClient == nil {
		return nil
	}
	if h.k8sClient.KubeClient != nil {
		return h.k8sClient.KubeClient
	}
	if h.k8sClient.Clientset != nil {
		return h.k8sClient.Clientset
	}
	return nil
}

func matchProjectCardJobEvidence(
	project *types.Project,
	services []*types.Service,
	argoEvidence *projectCardArgoApplicationEvidence,
	jobEvidenceByNamespace map[string]projectCardJobsEvidence,
) *projectCardJobsEvidence {
	if project == nil || len(jobEvidenceByNamespace) == 0 {
		return nil
	}

	namespaces := projectCardNamespaceCandidates(project, services, argoEvidence)
	matched := make([]projectCardJobsEvidence, 0, len(namespaces))
	for _, namespace := range namespaces {
		if evidence, ok := jobEvidenceByNamespace[namespace]; ok && evidence.CronJobCount > 0 {
			matched = append(matched, evidence)
		}
	}
	if len(matched) == 0 {
		return nil
	}

	combined := projectCardJobsEvidence{LastObservedAt: matched[0].LastObservedAt}
	for _, evidence := range matched {
		combined.NamespaceCount++
		combined.CronJobCount += evidence.CronJobCount
		combined.FailedCount += evidence.FailedCount
		combined.ActiveCount += evidence.ActiveCount
		combined.StuckCount += evidence.StuckCount
		combined.SucceededCount += evidence.SucceededCount
		if evidence.LastObservedAt.After(combined.LastObservedAt) {
			combined.LastObservedAt = evidence.LastObservedAt
		}
		combined.Items = append(combined.Items, evidence.Items...)
	}
	combined.Status = projectCardJobsStatus(combined)
	sort.Slice(combined.Items, func(i, j int) bool {
		if combined.Items[i].Status != combined.Items[j].Status {
			return projectCardJobStatusSeverity(combined.Items[i].Status) > projectCardJobStatusSeverity(combined.Items[j].Status)
		}
		if combined.Items[i].Namespace != combined.Items[j].Namespace {
			return combined.Items[i].Namespace < combined.Items[j].Namespace
		}
		return combined.Items[i].Name < combined.Items[j].Name
	})
	return &combined
}

func projectCardNamespaceCandidates(project *types.Project, services []*types.Service, argoEvidence *projectCardArgoApplicationEvidence) []string {
	seen := map[string]bool{}
	namespaces := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		namespaces = append(namespaces, value)
	}

	add(project.Slug)
	add(projectCardSanitizeArgoName(project.Name))
	if argoEvidence != nil {
		add(argoEvidence.DestinationNamespace)
	}
	for _, service := range services {
		if service == nil || service.K8sNamespace == nil {
			continue
		}
		add(*service.K8sNamespace)
	}
	return namespaces
}

func summarizeProjectCardCronJobs(cronJobs []batchv1.CronJob, jobs []batchv1.Job, observedAt time.Time) projectCardJobsEvidence {
	evidence := projectCardJobsEvidence{
		Status:         "empty",
		LastObservedAt: observedAt.UTC(),
		Items:          make([]projectCardJobEvidence, 0, len(cronJobs)),
	}
	if len(cronJobs) == 0 {
		return evidence
	}

	sort.Slice(cronJobs, func(i, j int) bool {
		if cronJobs[i].Namespace != cronJobs[j].Namespace {
			return cronJobs[i].Namespace < cronJobs[j].Namespace
		}
		return cronJobs[i].Name < cronJobs[j].Name
	})

	for _, cronJob := range cronJobs {
		item := summarizeProjectCardCronJob(cronJob, jobs, observedAt)
		evidence.CronJobCount++
		evidence.FailedCount += item.RecentFailedJobs
		evidence.ActiveCount += item.ActiveJobs
		evidence.StuckCount += item.StuckJobs
		evidence.SucceededCount += item.SucceededJobs
		if item.Status == "pending" {
			evidence.PendingCount++
		}
		evidence.Items = append(evidence.Items, item)
	}
	evidence.Status = projectCardJobsStatus(evidence)
	return evidence
}

func summarizeProjectCardCronJob(cronJob batchv1.CronJob, jobs []batchv1.Job, observedAt time.Time) projectCardJobEvidence {
	item := projectCardJobEvidence{
		Namespace:        cronJob.Namespace,
		Name:             cronJob.Name,
		Status:           "unknown",
		LastScheduleTime: projectCardMetaTimePtr(cronJob.Status.LastScheduleTime),
	}
	if cronJob.Spec.Suspend != nil && *cronJob.Spec.Suspend {
		item.Status = "suspended"
		return item
	}

	var latestJob *batchv1.Job
	var lastFailureTime *time.Time
	var recoveredAt *time.Time
	failureTimes := []*time.Time{}
	for i := range jobs {
		job := &jobs[i]
		if !projectCardJobOwnedByCronJob(job, cronJob.Name) {
			continue
		}
		if latestJob == nil || projectCardJobObservedAt(job).After(projectCardJobObservedAt(latestJob)) {
			latestJob = job
		}
		if job.Status.Active > 0 {
			item.ActiveJobs += int(job.Status.Active)
			activeObservedAt := projectCardJobObservedAt(job)
			recoveredAt = laterTime(recoveredAt, &activeObservedAt)
			if projectCardActiveJobIsStuck(job, cronJob, observedAt) {
				item.StuckJobs += int(job.Status.Active)
			}
		}
		if job.Status.Succeeded > 0 {
			item.SucceededJobs += int(job.Status.Succeeded)
			recoveredAt = laterTime(recoveredAt, projectCardJobSuccessTime(job))
		}
		if failureTime := projectCardJobFailureTime(job); failureTime != nil && observedAt.Sub(*failureTime) <= projectCardJobFailureWindow {
			failureTimes = append(failureTimes, failureTime)
		}
	}
	for _, failureTime := range failureTimes {
		if recoveredAt != nil && !failureTime.After(*recoveredAt) {
			continue
		}
		item.RecentFailedJobs++
		lastFailureTime = laterTime(lastFailureTime, failureTime)
	}
	if latestJob != nil {
		item.LatestJobName = latestJob.Name
		if item.LastScheduleTime == nil {
			item.LastScheduleTime = projectCardTimePtr(projectCardJobObservedAt(latestJob))
		}
	}
	item.LastFailureTime = lastFailureTime

	switch {
	case item.RecentFailedJobs > 0:
		item.Status = "failing"
	case item.StuckJobs > 0:
		item.Status = "degraded"
	case item.ActiveJobs > 0:
		item.Status = "active"
	case item.SucceededJobs > 0:
		item.Status = "healthy"
	case projectCardCronJobIsWaitingForFirstRun(cronJob, observedAt):
		item.Status = "pending"
	default:
		item.Status = "unknown"
	}
	return item
}

func projectCardJobsStatus(evidence projectCardJobsEvidence) string {
	switch {
	case evidence.CronJobCount == 0:
		return "empty"
	case evidence.FailedCount > 0:
		return "failing"
	case evidence.StuckCount > 0:
		return "degraded"
	case evidence.ActiveCount > 0:
		return "active"
	case evidence.SucceededCount > 0:
		return "healthy"
	case evidence.PendingCount > 0:
		return "pending"
	default:
		return "unknown"
	}
}

func projectCardJobStatusSeverity(status string) int {
	switch status {
	case "failing":
		return 5
	case "degraded":
		return 4
	case "active":
		return 3
	case "pending", "unknown":
		return 2
	case "healthy":
		return 1
	default:
		return 0
	}
}

func projectCardCronJobIsWaitingForFirstRun(cronJob batchv1.CronJob, observedAt time.Time) bool {
	if cronJob.Status.LastScheduleTime != nil {
		return false
	}
	createdAt := cronJob.CreationTimestamp.Time.UTC()
	if createdAt.IsZero() {
		return false
	}
	age := observedAt.Sub(createdAt)
	if age < 0 {
		age = 0
	}
	return age <= projectCardCronJobNoRunGrace
}

func projectCardJobOwnedByCronJob(job *batchv1.Job, cronJobName string) bool {
	if job == nil || cronJobName == "" {
		return false
	}
	for _, owner := range job.OwnerReferences {
		if owner.Kind == "CronJob" && owner.Name == cronJobName {
			return true
		}
	}
	return strings.HasPrefix(job.Name, cronJobName+"-")
}

func projectCardJobObservedAt(job *batchv1.Job) time.Time {
	if job == nil {
		return time.Time{}
	}
	if value := job.Annotations["batch.kubernetes.io/cronjob-scheduled-timestamp"]; value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.UTC()
		}
	}
	return job.CreationTimestamp.Time.UTC()
}

func projectCardJobFailureTime(job *batchv1.Job) *time.Time {
	if job == nil || job.Status.Failed == 0 {
		return nil
	}
	if job.Status.Succeeded > 0 || projectCardJobHasCondition(job, batchv1.JobComplete) {
		return nil
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == "True" {
			return projectCardMetaTimePtr(&condition.LastTransitionTime)
		}
	}
	if job.Status.Active > 0 {
		return nil
	}
	if job.Status.CompletionTime != nil {
		return projectCardMetaTimePtr(job.Status.CompletionTime)
	}
	return projectCardMetaTimePtr(&job.CreationTimestamp)
}

func projectCardJobSuccessTime(job *batchv1.Job) *time.Time {
	if job == nil || job.Status.Succeeded == 0 {
		return nil
	}
	if job.Status.CompletionTime != nil {
		return projectCardMetaTimePtr(job.Status.CompletionTime)
	}
	return projectCardTimePtr(projectCardJobObservedAt(job))
}

func projectCardJobHasCondition(job *batchv1.Job, conditionType batchv1.JobConditionType) bool {
	if job == nil {
		return false
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == conditionType && condition.Status == "True" {
			return true
		}
	}
	return false
}

func projectCardActiveJobIsStuck(job *batchv1.Job, cronJob batchv1.CronJob, observedAt time.Time) bool {
	if job == nil || job.Status.Active == 0 || job.Status.StartTime == nil {
		return false
	}
	age := observedAt.Sub(job.Status.StartTime.Time)
	if cronJob.Spec.JobTemplate.Spec.ActiveDeadlineSeconds != nil {
		return age > time.Duration(*cronJob.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)*time.Second
	}
	return age > projectCardJobActiveStaleAfter
}

func projectCardMetaTimePtr(value *metav1.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	return projectCardTimePtr(value.Time)
}

func projectCardTimePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
