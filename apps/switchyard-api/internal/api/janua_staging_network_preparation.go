package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagenetwork"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

type januaNetworkRequest struct {
	ID        string `json:"operation_id"`
	Authority string `json:"authority_id"`
	DryRun    *bool  `json:"dry_run"`
	Expected  string `json:"expect_plan_sha256,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func parseJanuaNetworkRequest(raw []byte) (januaNetworkRequest, bool) {
	var r januaNetworkRequest
	var keys map[string]json.RawMessage
	if len(raw) > 8192 || k8s.DecodeInitialBootstrapJSON(raw, &keys) != nil || k8s.DecodeInitialBootstrapJSON(raw, &r) != nil || r.DryRun == nil {
		return r, false
	}
	for key := range keys {
		if key != "operation_id" && key != "authority_id" && key != "dry_run" && key != "expect_plan_sha256" && key != "reason" {
			return r, false
		}
	}
	if _, ok := canonicalStagingIntentID(r.ID); !ok {
		return r, false
	}
	if _, ok := canonicalStagingIntentID(r.Authority); !ok {
		return r, false
	}
	if *r.DryRun {
		return r, len(keys) == 3
	}
	return r, len(keys) == 5 && januaStagingIntentHash.MatchString(r.Expected) && r.Reason != "" && len(r.Reason) <= 512 && strings.TrimSpace(r.Reason) == r.Reason && strings.IndexFunc(r.Reason, unicode.IsControl) < 0
}

func registerJanuaNetworkPreparationRoutes(platform *gin.RouterGroup, h *Handler) {
	base := "/projects/:slug/environments/staging/network-preparation"
	platform.POST(base, h.RequirePlatformOperator(), h.JanuaNetworkPreparation)
	platform.GET(base+"/:operation_id", h.RequirePlatformOperator(), h.JanuaNetworkPreparationStatus)
}

func (h *Handler) readJanuaNetworkInputs(ctx context.Context, a januastage.Authority) (januastagenetwork.Inputs, error) {
	var in januastagenetwork.Inputs
	ns, e := h.readJanuaStageNamespace(ctx, januastage.Namespace, a.ProjectID)
	if e != nil || ns.State != "OWNED" || ns.UID != a.NamespaceUID || ns.ResourceVersion == "" || !ns.SignatureAdmissionLabel || ns.DNSOnlyBaseline != "EXACT_OWNED" || ns.BaselineResourceVersion == "" || ns.LegacySharedDataAccessLabel || ns.LegacySharedPostgresConsumerLabel {
		return in, januastagenetwork.ErrRefused
	}
	peers, peerVersions, e := h.readJanuaStageAPIPeers(ctx)
	if e != nil {
		return in, januastagenetwork.ErrRefused
	}
	operator, operatorVersions, e := h.readJanuaStageOperator(ctx)
	if e != nil {
		return in, januastagenetwork.ErrRefused
	}
	for _, p := range peers {
		in.Peers = append(in.Peers, januastagenetwork.Peer{HostRoute: p.HostRoute, Port: p.Port})
	}
	in.Operator = operator
	// Exclude additive policies created by this operation; Native.Guard closes
	// their exact inventory/UIDs. The original namespace and baseline bind here.
	in.ContextSHA256 = januastagenetwork.Hash(struct {
		NamespaceUID, NamespaceVersion, BaselineUID, BaselineVersion string
		PeerVersions, OperatorVersions                               []string
	}{ns.UID, ns.ResourceVersion, ns.BaselineUID, ns.BaselineResourceVersion, peerVersions, operatorVersions})
	return in, nil
}
func (h *Handler) januaNetworkNative() januastagenetwork.NativeAdapter {
	return januastagenetwork.NativeAdapter{Kube: h.k8sClient.Kube(), Dynamic: h.k8sClient.DynamicClient, Fresh: func(ctx context.Context, p januastagenetwork.Plan) error {
		if (januastagenetwork.Store{DB: h.repos.DB()}).Check(ctx, p) != nil {
			return januastagenetwork.ErrRefused
		}
		a, e := h.readJanuaInertAuthority(ctx, p.Authority.ID)
		if e != nil {
			return januastagenetwork.ErrRefused
		}
		stamp, e := time.Parse(time.RFC3339Nano, a.ObservedAt)
		if e != nil || time.Since(stamp) < 0 || time.Since(stamp) > 30*time.Second {
			return januastagenetwork.ErrRefused
		}
		in, e := h.readJanuaNetworkInputs(ctx, a)
		if e != nil {
			return e
		}
		current, e := januastagenetwork.BuildPlan(p.ID, p.ActorID, a, in)
		if e != nil || current.SHA256 != p.SHA256 {
			return januastagenetwork.ErrRefused
		}
		return nil
	}}
}

var januaNetworkRemaining = []string{"NATIVE_ALLOW_DENY_PACKET_PROBE_MISSING", "EXACT_ALLOCATION_SCHEDULER_AND_RETAINED_STORAGE_PLACEMENT_MISSING", "OPERATION_SPECIFIC_COST_APPROVAL_AND_FRESH_METERED_SPEND_MISSING", "DEDICATED_REGISTRY_CUSTODY_MISSING", "DB_AND_MIGRATION_ADMISSION_READINESS_AND_RUNTIME_CUSTODY_MISSING", "STAGING_ACTIVATION_TRANSITION_AND_SYNTHETIC_AUTH_SOAK_MISSING"}

func januaNetworkResponse(p januastagenetwork.Plan, r januastagenetwork.Receipt, dry, observed bool) gin.H {
	if r.Objects == nil {
		r.Objects = make([]januastagenetwork.ObjectReceipt, 0)
	}
	objects := make([]gin.H, 0, len(p.Objects()))
	for i, o := range p.Objects() {
		objects = append(objects, gin.H{"object_key": o.Key, "desired_sha256": p.ObjectSHA256[i]})
	}
	// Never serialize Plan: canonical API endpoint identities are private.
	return gin.H{"schema": "enclii-janua-network-preparation-v1", "operation_id": p.ID, "authority_id": p.Authority.ID, "dry_run": dry, "plan_sha256": p.SHA256, "objects": objects, "create_intents": r.Attempted, "receipts": r.Objects, "completion_recorded": r.Complete, "configuration_present": observed, "packet_isolation_proven": false, "db_apply_allowed": false, "activation_held": true, "unresolved_gates": januaNetworkRemaining}
}
func (h *Handler) JanuaNetworkPreparation(c *gin.Context) {
	raw, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	r, ok := parseJanuaNetworkRequest(raw)
	actor, actorOK := canonicalStagingIntentID(c.GetString("user_id"))
	if e != nil || !ok || !actorOK {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed_network_preparation_request_required"})
		return
	}
	if _, ok = h.januaStagingIntentStore(c); !ok {
		return
	}
	if h.k8sClient == nil || h.k8sClient.Kube() == nil || h.k8sClient.DynamicClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "network_preparation_adapter_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	id, _ := uuid.Parse(r.ID)
	authorityID, _ := uuid.Parse(r.Authority)
	a, e := h.readJanuaInertAuthority(ctx, authorityID)
	if e != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "current_held_native_authority_required"})
		return
	}
	in, e := h.readJanuaNetworkInputs(ctx, a)
	p, planErr := januastagenetwork.BuildPlan(id, actor, a, in)
	store := januastagenetwork.Store{DB: h.repos.DB()}
	if e != nil || planErr != nil || store.Check(ctx, p) != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "current_owned_network_plan_required"})
		return
	}
	native := h.januaNetworkNative()
	c.Header("Cache-Control", "no-store")
	if *r.DryRun {
		if native.Guard(ctx, p, januastagenetwork.Receipt{Plan: p}) != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "create_only_empty_native_targets_required"})
			return
		}
		c.JSON(http.StatusOK, januaNetworkResponse(p, januastagenetwork.Receipt{}, true, false))
		return
	}
	receipt, e := (januastagenetwork.Kernel{Native: native, Journal: store}).Apply(ctx, p, r.Expected, r.Reason)
	if e != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "network_preparation_unconfirmed_inspect_same_operation", "operation_id": id, "activation_held": true})
		return
	}
	c.JSON(http.StatusOK, januaNetworkResponse(p, receipt, false, true))
}
func (h *Handler) JanuaNetworkPreparationStatus(c *gin.Context) {
	id, ok := canonicalStagingIntentID(c.Param("operation_id"))
	actor, actorOK := canonicalStagingIntentID(c.GetString("user_id"))
	if !ok || !actorOK || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exact_network_operation_required"})
		return
	}
	if _, ok = h.januaStagingIntentStore(c); !ok {
		return
	}
	if h.k8sClient == nil || h.k8sClient.Kube() == nil || h.k8sClient.DynamicClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "network_preparation_adapter_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	store := januastagenetwork.Store{DB: h.repos.DB()}
	r, e := store.Read(ctx, id, actor)
	if e != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "network_operation_unavailable"})
		return
	}
	observed := (januastagenetwork.Kernel{Native: h.januaNetworkNative(), Journal: store}).Observe(ctx, r) == nil
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, januaNetworkResponse(r.Plan, r, false, observed))
}
