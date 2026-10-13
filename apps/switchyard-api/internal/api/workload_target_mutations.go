package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

// The HTTP result is held until the lifecycle lock transaction commits. An
// uncertain commit must not flush a buffered success response.
type targetResponse struct {
	gin.ResponseWriter
	body    bytes.Buffer
	status  int
	written bool
}

func (w *targetResponse) WriteHeader(code int) {
	if !w.written {
		w.status = code
	}
}
func (w *targetResponse) WriteHeaderNow() { w.written = true }
func (w *targetResponse) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	return w.body.Write(data)
}
func (w *targetResponse) WriteString(data string) (int, error) {
	w.WriteHeaderNow()
	return w.body.WriteString(data)
}
func (w *targetResponse) Status() int { return w.status }
func (w *targetResponse) Size() int {
	if !w.written {
		return -1
	}
	return w.body.Len()
}
func (w *targetResponse) Written() bool { return w.written }
func (w *targetResponse) Flush()        { w.WriteHeaderNow() }

type targetLease struct {
	handler  *Handler
	session  *workloadtargets.Session
	response *targetResponse
}

func (h *Handler) targetLease(c *gin.Context, id uuid.UUID, project bool, refuseBound bool) (*targetLease, bool) {
	var session *workloadtargets.Session
	var err error
	store := workloadtargets.Store{DB: h.repos.DB()}
	if project {
		session, err = store.LockProject(c.Request.Context(), id)
	} else {
		session, err = store.LockService(c.Request.Context(), id)
	}
	if err == nil && refuseBound {
		err = session.RefuseBound(c.Request.Context())
	}
	if err != nil {
		if session != nil {
			session.Close()
		}
		c.JSON(targetMutationStatus(err), gin.H{"error": "explicit deployment target conflicts with this legacy mutation; no effect attempted"})
		return nil, false
	}
	scoped := *h
	scoped.repos = h.repos.InTransaction(session.Tx())
	scoped.workloadTargetSession = session
	response := &targetResponse{ResponseWriter: c.Writer, status: http.StatusOK}
	c.Writer = response
	return &targetLease{handler: &scoped, session: session, response: response}, true
}
func (l *targetLease) finish(c *gin.Context) {
	defer l.session.Close()
	original := l.response.ResponseWriter
	c.Writer = original
	if !l.response.Written() {
		return
	} // panic recovery remains the outer middleware's responsibility
	if l.response.status < 400 {
		if err := l.session.Commit(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "mutation outcome unknown; inspect exact service before another operation"})
			return
		}
	}
	original.WriteHeader(l.response.status)
	_, _ = original.Write(l.response.body.Bytes())
}

// mutateVerifiedTarget uses the same service lock for absent and bound records.
// It never resolves a Deployment by observation labels or a name suffix.
func (h *Handler) mutateVerifiedTarget(ctx context.Context, serviceID uuid.UUID, environment string, replicas *int32) error {
	if h.workloadTargetSession == nil {
		return workloadtargets.ErrUnavailable
	}
	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		return workloadtargets.ErrIntent
	}
	boundErr := h.workloadTargetSession.RefuseBound(ctx)
	if boundErr == nil {
		// A genuinely unbound service retains the original exact name/namespace
		// behavior, including the legacy environment field's existing semantics.
		project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID)
		if err != nil {
			return workloadtargets.ErrIntent
		}
		if replicas != nil {
			return h.k8sClient.ScaleDeployment(ctx, project.Slug, svc.Name, *replicas)
		}
		return h.k8sClient.RollingRestart(ctx, project.Slug, svc.Name)
	}
	if !errors.Is(boundErr, workloadtargets.ErrConflict) {
		return boundErr
	}
	env, err := h.repos.Environments.GetByProjectAndName(svc.ProjectID, environment)
	if err != nil {
		return workloadtargets.ErrIntent
	}
	binding, err := h.workloadTargetStore().Get(ctx, serviceID, env.ID)
	if err != nil {
		return err
	}
	if binding == nil {
		return workloadtargets.ErrConflict
	} // no other-environment legacy fallback

	actor, err := uuid.Parse(actorFromTargetContext(ctx))
	if err != nil {
		return workloadtargets.ErrOperator
	}
	if err = h.workloadTargetSession.RequireOperator(ctx, actor); err != nil {
		return err
	}
	if err = h.workloadTargetSession.LockIntent(ctx, binding.Identity.EnvironmentID, binding.Identity.RegistrationID); err != nil {
		return err
	}
	project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID)
	if err != nil {
		return workloadtargets.ErrIntent
	}
	request := workloadtargets.Request{Project: project.Slug, ServiceID: serviceID, Environment: environment, Namespace: binding.Identity.Namespace, Deployment: binding.Identity.Deployment, DryRun: true}
	snapshot, err := h.targetSnapshot(ctx, request)
	if err != nil {
		return err
	}
	// Force the store's binding into the projection; an injected metadata reader
	// cannot omit it and thereby permit UID or configured authority refresh.
	snapshot.Incumbent = binding
	plan, err := workloadtargets.ValidateSnapshot(snapshot, request.Namespace, request.Deployment)
	if err != nil {
		return err
	}
	if h.opsKubeClient() == nil {
		return workloadtargets.ErrUnavailable
	}
	deployment := snapshot.Deployment.DeepCopy()
	if replicas != nil {
		deployment.Spec.Replicas = replicas
	} else {
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	// Full PUT on the object just verified: UID, resourceVersion and exact name
	// remain unchanged. No unchecked second GET, patch or retry follows.
	restClient := h.opsKubeClient().AppsV1().RESTClient()
	if restClient == nil {
		return workloadtargets.ErrUnavailable
	}
	updated := &appsv1.Deployment{}
	err = restClient.Put().Namespace(request.Namespace).Resource("deployments").Name(request.Deployment).VersionedParams(&metav1.UpdateOptions{}, scheme.ParameterCodec).Body(deployment).MaxRetries(0).Do(ctx).Into(updated)
	if err != nil {
		return err
	}
	if updated == nil || string(updated.UID) != plan.Identity.DeploymentUID || updated.Namespace != request.Namespace || updated.Name != request.Deployment {
		return workloadtargets.ErrConflict
	}
	action := "service.scaled"
	if replicas == nil {
		action = "service.restarted"
	}
	if h.repos.AuditLogs == nil {
		return workloadtargets.ErrUnavailable
	}
	// Record the fresh read versions, not immutable enrollment versions. Native
	// success followed by audit/commit uncertainty requires inspection, no retry.
	return h.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID: &actor, ActorRole: types.RoleAdmin, Action: action, ResourceType: "service", ResourceID: serviceID.String(), ResourceName: svc.Name,
		ProjectID: &svc.ProjectID, EnvironmentID: &env.ID, Outcome: "success",
		Context:  map[string]interface{}{"binding_id": binding.ID, "namespace": request.Namespace, "deployment": request.Deployment},
		Metadata: map[string]interface{}{"verified_plan_sha256": plan.SHA, "reviewed_resource_versions": plan.Versions, "namespace_uid": plan.Identity.NamespaceUID, "deployment_uid": plan.Identity.DeploymentUID, "application_uid": plan.Identity.ApplicationUID, "argo_config_uid": plan.Identity.ArgoConfigUID, "tracking_method": plan.Identity.TrackingMethod, "installation_id": plan.Identity.InstallationID, "source_revision": plan.Identity.Revision, "updated_resource_version": updated.ResourceVersion},
	})
}

type targetActorKey struct{}

func withTargetActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, targetActorKey{}, actor)
}
func actorFromTargetContext(ctx context.Context) string {
	actor, _ := ctx.Value(targetActorKey{}).(string)
	return actor
}
