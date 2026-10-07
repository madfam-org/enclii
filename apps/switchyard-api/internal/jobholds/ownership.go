package jobholds

import (
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
)

func Validate(job *batchv1.CronJob, b Binding, namespace string) error {
	if job.Namespace != namespace || job.Labels["enclii.dev/managed-by"] != "switchyard" ||
		job.Labels["enclii.dev/service"] != b.ServiceName || job.Labels["enclii.dev/project"] != b.ProjectID.String() ||
		job.Labels["enclii.dev/cron-job-id"] != "" {
		return fmt.Errorf("CronJob does not match the requested Switchyard service, project and namespace")
	}
	return nil
}

func Apply(job *batchv1.CronJob) {
	suspended := true
	job.Spec.Suspend = &suspended
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	job.Annotations[Annotation] = "true"
}
