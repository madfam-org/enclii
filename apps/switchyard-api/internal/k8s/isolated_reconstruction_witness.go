package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// PortalFenceOrigin is resolved by the server while the original kernel is
// still bound. It is retained with the read's immutable intent, never supplied
// by a caller or reconstructed from whichever addon happens to be active later.
type PortalFenceOrigin struct {
	KernelID           string    `json:"kernel_id"`
	ServiceID          uuid.UUID `json:"service_id"`
	AddonID            uuid.UUID `json:"addon_id"`
	BindingID          uuid.UUID `json:"binding_id"`
	DeploymentUID      string    `json:"deployment_uid"`
	WorkloadServiceUID string    `json:"workload_service_uid"`
	ClusterUID         string    `json:"cluster_uid"`
	Image              string    `json:"image"`
}

func validFenceOrigin(origin *PortalFenceOrigin, input *PortalFenceReadInput) bool {
	if origin == nil || !validFenceInput(input) || origin.KernelID != input.KernelID || origin.ServiceID == uuid.Nil || origin.AddonID == uuid.Nil || origin.BindingID == uuid.Nil {
		return false
	}
	for _, uid := range []string{origin.DeploymentUID, origin.WorkloadServiceUID, origin.ClusterUID} {
		if !initialUID.MatchString(uid) {
			return false
		}
	}
	prefix := "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:"
	return strings.HasPrefix(origin.Image, prefix) && initialSHA.MatchString(strings.TrimPrefix(origin.Image, prefix))
}

// Legacy observations may lack an origin witness; they remain readable, but
// cannot authorize a close. No other profile may carry a fence-origin witness.
func compatibleFenceOrigin(profile string, input *PortalFenceReadInput, origin *PortalFenceOrigin) bool {
	if origin == nil {
		return true
	}
	return profile == PortalFenceReadProfile && validFenceOrigin(origin, input)
}

// RetainedBootstrapEvidence is a server-only projection of validated immutable
// records. It contains no transport credential or caller-selected payload.
type RetainedBootstrapEvidence struct {
	OperationSHA256, ReviewedPlanSHA256 string
	CloseInput                          *PortalReconstructionCloseInput
	CloseEvidence                       *PortalReconstructionEvidence

	ProjectID, AddonID, ServiceID                          uuid.UUID
	Namespace, Profile, Image, IntentSHA256, ReceiptSHA256 string
	CreatedAt                                              time.Time
	Identity                                               InitialBootstrapIdentity
	FenceOrigin                                            *PortalFenceOrigin
	Native                                                 *InitialNativeReceipt
}

func (c *Client) RetainedBootstrapEvidence(ctx context.Context, namespace, name string, project, addon uuid.UUID) (*RetainedBootstrapEvidence, error) {
	bad := fmt.Errorf("retained bootstrap evidence unavailable")
	receipt, _, err := c.retainedInitialStatus(ctx, namespace, name, project, addon)
	if err != nil || receipt == nil || receipt.Native == nil {
		return nil, bad
	}
	cm, err := c.kubeClient().CoreV1().ConfigMaps(namespace).Get(ctx, name+"-intent", metav1.GetOptions{})
	if err != nil {
		return nil, bad
	}
	var in initialIntent
	if DecodeInitialBootstrapJSON([]byte(cm.Data["payload"]), &in) != nil {
		return nil, bad
	}
	p := IsolatedInitialJobPlan{ProjectID: project, AddonID: addon, ServiceID: in.ServiceID, Namespace: namespace, Cluster: in.Cluster, Profile: in.Profile, Image: in.Image, Identity: in.Identity, GenerationRead: in.GenerationRead, FenceRead: in.FenceRead, FenceOrigin: in.FenceOrigin, ReconstructionClose: in.ReconstructionClose, ReconstructionEvidence: in.ReconstructionEvidence, retainedJobName: name}
	if validateInitialStatusIntent(cm, p, name, &in) != nil {
		return nil, bad
	}
	native, err := c.readInitialReceipt(ctx, p, cm)
	if err != nil || native.Classification != p.kind().retained {
		return nil, bad
	}
	kept, err := c.kubeClient().CoreV1().ConfigMaps(namespace).Get(ctx, name+"-receipt", metav1.GetOptions{})
	if err != nil || !initialSHA.MatchString(kept.Data["sha256"]) {
		return nil, bad
	}
	planned := in
	planned.JobUID = ""
	planned.CreationResourceVersion = ""
	planBytes, err := json.Marshal(planned)
	if err != nil {
		return nil, bad
	}
	return &RetainedBootstrapEvidence{OperationSHA256: in.OperationSHA256, ReviewedPlanSHA256: initialHash(planBytes), CloseInput: in.ReconstructionClose, CloseEvidence: in.ReconstructionEvidence, ProjectID: project, AddonID: addon, ServiceID: in.ServiceID, Namespace: namespace, Profile: in.Profile, Image: in.Image, IntentSHA256: cm.Data["sha256"], ReceiptSHA256: kept.Data["sha256"], CreatedAt: cm.CreationTimestamp.Time, Identity: in.Identity, FenceOrigin: in.FenceOrigin, Native: native}, nil
}

// InitialMAPEvidence derives the fixed initial operation name from its owned
// IDs; it never trusts a caller's choice of an initial receipt.
func (c *Client) InitialMAPEvidence(ctx context.Context, namespace string, project, addon, service uuid.UUID, profile string) (*RetainedBootstrapEvidence, error) {
	if !IsMapSimProfile(profile) {
		return nil, fmt.Errorf("owned initial MAP evidence required")
	}
	p := IsolatedInitialJobPlan{ProjectID: project, AddonID: addon, ServiceID: service, Profile: profile}
	evidence, err := c.RetainedBootstrapEvidence(ctx, namespace, p.jobName(), project, addon)
	if err != nil || evidence.Profile != profile || evidence.ServiceID != service || evidence.Native.MAP == nil {
		return nil, fmt.Errorf("owned initial MAP evidence required")
	}
	return evidence, nil
}

// AppliedPortalCloseSchema returns an accredited step004 receipt on this exact
// addon/Cluster. A full bounded list is required; a partial listing never means
// that no matching evidence exists.
func (c *Client) AppliedPortalCloseSchema(ctx context.Context, namespace string, project, addon, service uuid.UUID, clusterUID string) (*RetainedBootstrapEvidence, error) {
	selector := labels.Set{"enclii.dev/managed-by": fixedStepKind.manager, LabelProjectNamespace: project.String(), "enclii.dev/addon-id": addon.String(), "enclii.dev/service-id": service.String(), "enclii.dev/bootstrap-profile": fixedStepCierreReconstruccionPasoProfile}.AsSelector().String()
	records, err := c.kubeClient().CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 100})
	if err != nil || records.Continue != "" || len(records.Items) > 100 {
		return nil, fmt.Errorf("step004 evidence unavailable")
	}
	var result *RetainedBootstrapEvidence
	for _, cm := range records.Items {
		if !strings.HasSuffix(cm.Name, "-intent") || cm.Immutable == nil || !*cm.Immutable || cm.UID == "" {
			continue
		}
		evidence, e := c.RetainedBootstrapEvidence(ctx, namespace, strings.TrimSuffix(cm.Name, "-intent"), project, addon)
		if e != nil || evidence.Profile != fixedStepCierreReconstruccionPasoProfile || evidence.ServiceID != service || evidence.Identity.ClusterUID != clusterUID || !validCierreReconstruccionPaso(evidence.Native.CierreReconstruccionPaso) || evidence.Native.CierreReconstruccionPaso.SchemaSHA256 != PortalGenerationSchemaSHA256 {
			continue
		}
		if result == nil || evidence.CreatedAt.After(result.CreatedAt) {
			result = evidence
		}
	}
	if result == nil {
		return nil, fmt.Errorf("step004 evidence unavailable")
	}
	return result, nil
}

func reconstructionOriginHash(profile string, origin *PortalFenceOrigin) string {
	if profile != PortalFenceReadProfile || origin == nil {
		return ""
	}
	b, _ := json.Marshal(origin)
	return initialHash(b)
}
