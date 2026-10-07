package reconciler

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/jobholds"
	batchv1 "k8s.io/api/batch/v1"
)

func (r *ServiceReconciler) applyHeldCronJob(ctx context.Context, req *ReconcileRequest, job *batchv1.CronJob) error {
	binding := jobholds.Binding{ProjectID: req.Service.ProjectID, ServiceID: req.Service.ID, EnvironmentID: req.Environment.ID, ServiceName: req.Service.Name}
	if err := jobholds.Validate(job, binding, req.Environment.KubeNamespace); err != nil {
		return err
	}
	return r.jobHolds.WithLock(ctx, job.Namespace, job.Name, func(ctx context.Context, session *jobholds.Session) error {
		hold, err := session.Get(ctx, job.Namespace, job.Name)
		if err != nil {
			return fmt.Errorf("read job hold: %w", err)
		}
		if hold != nil {
			if hold.ServiceID != req.Service.ID || hold.ProjectID != req.Service.ProjectID || hold.EnvironmentID != req.Environment.ID {
				return fmt.Errorf("job hold belongs to another service or environment")
			}
			if err := jobholds.Validate(job, hold.Binding, req.Environment.KubeNamespace); err != nil {
				return err
			}
			jobholds.Apply(job)
		}
		return r.applyCronJob(ctx, job)
	})
}
