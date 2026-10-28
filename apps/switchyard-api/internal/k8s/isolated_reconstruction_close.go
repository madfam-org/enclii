package k8s

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var reconstructionReadName = regexp.MustCompile(`^read-[a-f0-9]{24}$`)

const PortalReconstructionCloseProfile = "portal-sim-cierre-reconstruccion"
const PortalReconstructionCloserSHA256 = "8863e97b89f889ec88f329c1c4994d8770ea575429569f1c81d9eb49dccc0b76" // pragma: allowlist secret -- public source digest
const portalReconstructionCloseCommand = "exec node /app/packages/entrenamiento/src/cerrar-por-reconstruccion.ts < /var/run/crea-sim/request/request.json"

type ReconstructionInfrastructure struct {
	ProjectID             uuid.UUID `json:"projectId"`
	ServiceID             uuid.UUID `json:"serviceId"`
	PreviousAddonID       uuid.UUID `json:"addonAnteriorId"`
	NewAddonID            uuid.UUID `json:"addonNuevoId"`
	NewAddonCreatedAt     string    `json:"addonNuevoCreadoEn"`
	Image                 string    `json:"mapImage"`
	BindingEvidenceSHA256 string    `json:"enlaceEvidenciaSha256"`
	SeedEvidenceSHA256    string    `json:"siembraEvidenciaSha256"`
}
type ReconstructionEmptySeed struct {
	Revision            string `json:"revision"`
	Epoch               string `json:"epoca"`
	AccreditationSHA256 string `json:"acreditacionSha256"`
	CodeSHA256          string `json:"codigoSha256"`
	FixtureSHA256       string `json:"fixtureSha256"`
	Roles               int    `json:"roles"`
	Cases               int    `json:"casos"`
}
type ReconstructionPortalDestination struct {
	AddonID        uuid.UUID `json:"portalAddonId"`
	NamespaceUID   string    `json:"portalNamespaceUid"`
	ClusterUID     string    `json:"portalClusterUid"`
	ServiceUID     string    `json:"portalServiceUid"`
	DeploymentUID  string    `json:"portalDeploymentUid"`
	Image          string    `json:"portalImage"`
	SourceRevision string    `json:"portalSourceSha"`
	SchemaSHA256   string    `json:"schemaSha256"`
}
type PortalReconstructionCloseInput struct {
	Version        string                          `json:"version"`
	CloseID        uuid.UUID                       `json:"cierreId"`
	ReservationID  uuid.UUID                       `json:"reservaId"`
	KernelID       string                          `json:"nucleoId"`
	FenceSHA256    string                          `json:"frenoSha256"`
	Infrastructure ReconstructionInfrastructure    `json:"infra"`
	Empty          ReconstructionEmptySeed         `json:"vacio"`
	Destination    ReconstructionPortalDestination `json:"destino"`
}

// Evidence links are retained by Enclii, not accepted from the HTTP caller or
// passed as an assertion of readiness to the portal process.
type PortalReconstructionEvidence struct {
	FenceReadJob                 string `json:"fence_read_job"`
	FenceIntentSHA256            string `json:"fence_intent_sha256"`
	FenceReceiptSHA256           string `json:"fence_receipt_sha256"`
	SeedIntentSHA256             string `json:"seed_intent_sha256"`
	SeedReceiptSHA256            string `json:"seed_receipt_sha256"`
	SchemaReceiptSHA256          string `json:"schema_receipt_sha256"`
	PortalDatabaseIdentitySHA256 string `json:"portal_database_identity_sha256"`
}
type PortalReconstructionCloseReceipt struct {
	Source                 string    `json:"fuente"`
	Version                string    `json:"version"`
	State                  string    `json:"estado"`
	CloseID                uuid.UUID `json:"cierreId"`
	ReservationID          uuid.UUID `json:"reservaId"`
	PayloadSHA256          string    `json:"payloadSha256"`
	SchemaSHA256           string    `json:"schemaSha256"`
	DatabaseIdentitySHA256 string    `json:"databaseIdentitySha256"`
}

func validReconstructionInput(p *PortalReconstructionCloseInput) bool {
	if p == nil || p.Version != "crea-sim-cierre-reconstruccion-v1" || p.CloseID == uuid.Nil || p.ReservationID == uuid.Nil || !validFenceInput(&PortalFenceReadInput{Version: "crea-sim-fence-kernel-read-v1", KernelID: p.KernelID}) {
		return false
	}
	i, x, d := p.Infrastructure, p.Empty, p.Destination
	if i.ProjectID == uuid.Nil || i.ServiceID == uuid.Nil || i.PreviousAddonID == uuid.Nil || i.NewAddonID == uuid.Nil || i.PreviousAddonID == i.NewAddonID || d.AddonID == uuid.Nil || x.Revision != "0" || x.Epoch != "0" || x.Roles != 7 || (x.Cases != 6 && x.Cases != 52) || d.SchemaSHA256 != PortalGenerationSchemaSHA256 || !initialRevision.MatchString(d.SourceRevision) {
		return false
	}
	at, err := time.Parse("2006-01-02T15:04:05.000Z", i.NewAddonCreatedAt)
	if err != nil || at.Format("2006-01-02T15:04:05.000Z") != i.NewAddonCreatedAt {
		return false
	}
	for _, hash := range []string{p.FenceSHA256, i.BindingEvidenceSHA256, i.SeedEvidenceSHA256, x.AccreditationSHA256, x.CodeSHA256, x.FixtureSHA256} {
		if !initialSHA.MatchString(hash) {
			return false
		}
	}
	for _, uid := range []string{d.NamespaceUID, d.ClusterUID, d.ServiceUID, d.DeploymentUID} {
		if !initialUID.MatchString(uid) {
			return false
		}
	}
	for image, prefix := range map[string]string{i.Image: "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:", d.Image: "ghcr.io/madfam-org/crea-sim/gateway@sha256:"} {
		if !strings.HasPrefix(image, prefix) || !initialSHA.MatchString(strings.TrimPrefix(image, prefix)) {
			return false
		}
	}
	return true
}
func compatibleReconstructionClose(p IsolatedInitialJobPlan) bool {
	if p.Profile != PortalReconstructionCloseProfile {
		return p.ReconstructionClose == nil && p.ReconstructionEvidence == nil
	}
	input, e := p.ReconstructionClose, p.ReconstructionEvidence
	if !validReconstructionInput(input) || e == nil || !reconstructionReadName.MatchString(e.FenceReadJob) {
		return false
	}
	for _, hash := range []string{e.FenceIntentSHA256, e.FenceReceiptSHA256, e.SeedIntentSHA256, e.SeedReceiptSHA256, e.SchemaReceiptSHA256, e.PortalDatabaseIdentitySHA256} {
		if !initialSHA.MatchString(hash) {
			return false
		}
	}
	d := input.Destination
	return input.Infrastructure.ProjectID == p.ProjectID && input.Infrastructure.ServiceID != p.ServiceID && d.AddonID == p.AddonID && d.NamespaceUID == p.Identity.ApplicationNamespaceUID && d.ClusterUID == p.Identity.ClusterUID && d.ServiceUID == p.Identity.WorkloadServiceUID && d.DeploymentUID == p.Identity.DeploymentUID && d.Image == p.Image && d.SourceRevision == p.Identity.SourceRevision && input.Infrastructure.SeedEvidenceSHA256 == e.SeedReceiptSHA256
}

// The product hashes recursively sorted JSON. All data are closed ASCII IDs,
// hashes and metadata, so Go's sorted-map encoding is its canonical byte form.
func ReconstructionCloseBytes(p *PortalReconstructionCloseInput) ([]byte, error) {
	if !validReconstructionInput(p) {
		return nil, fmt.Errorf("closed reconstruction input refused")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var sorted map[string]any
	if err = json.Unmarshal(b, &sorted); err != nil {
		return nil, err
	}
	return json.Marshal(sorted)
}
func validReconstructionReceipt(r *PortalReconstructionCloseReceipt) bool {
	return r != nil && r.Source == PortalReconstructionCloseProfile && r.Version == "crea-sim-cierre-reconstruccion-v1" && (r.State == "CERRADO" || r.State == "YA_CERRADO") && r.CloseID != uuid.Nil && r.ReservationID != uuid.Nil && initialSHA.MatchString(r.PayloadSHA256) && r.SchemaSHA256 == PortalGenerationSchemaSHA256 && initialSHA.MatchString(r.DatabaseIdentitySHA256)
}
func reconstructionReceiptMatches(r *PortalReconstructionCloseReceipt, p IsolatedInitialJobPlan) bool {
	if !compatibleReconstructionClose(p) || !validReconstructionReceipt(r) {
		return false
	}
	b, err := ReconstructionCloseBytes(p.ReconstructionClose)
	return err == nil && r.CloseID == p.ReconstructionClose.CloseID && r.ReservationID == p.ReconstructionClose.ReservationID && r.PayloadSHA256 == initialHash(b) && r.DatabaseIdentitySHA256 == p.ReconstructionEvidence.PortalDatabaseIdentitySHA256
}
func (p IsolatedInitialJobPlan) ReconstructionPlanSHA256() (string, error) {
	if p.Profile != PortalReconstructionCloseProfile || !compatibleReconstructionClose(p) {
		return "", fmt.Errorf("reconstruction authority refused")
	}
	job, err := BuildIsolatedInitialJob(p)
	if err != nil {
		return "", err
	}
	in, err := p.initialIntent(job)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	return initialHash(b), nil
}

func ReconstructionCloseJobName(project, addon, service uuid.UUID, key string) string {
	return (IsolatedInitialJobPlan{ProjectID: project, AddonID: addon, ServiceID: service, Profile: PortalReconstructionCloseProfile, IdempotencyKey: key}).jobName()
}
