package k8s

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// PortalFenceReadProfile resolves one retained fenced reservation from its
// closed kernel ID. It observes metadata only and cannot close or reset it.
const PortalFenceReadProfile = "portal-sim-reset-fence-read"

// Public source hash of the reviewed fixed reader; not a credential.
const PortalFenceReaderSHA256 = "ee9aea0ecb93847ffef4b1f544c62f157c3356b9d34ac5d0b0e007a3712b07c7" // pragma: allowlist secret
const portalFenceReadCommand = "exec node /app/packages/entrenamiento/src/leer-freno-reset.ts < /var/run/crea-sim/request/request.json"

type PortalFenceReadInput struct {
	Version  string `json:"version"`
	KernelID string `json:"nucleoId"`
}

func validFenceInput(p *PortalFenceReadInput) bool {
	return p != nil && p.Version == "crea-sim-fence-kernel-read-v1" &&
		(p.KernelID == "crea-sim-map01" || p.KernelID == "crea-sim-map02" || p.KernelID == "crea-sim-map03")
}
func ValidatePortalFenceReadInput(p *PortalFenceReadInput) error {
	if !validFenceInput(p) {
		return fmt.Errorf("closed fence read input refused")
	}
	return nil
}

type PortalFenceReadReceipt struct {
	Source              string `json:"fuente"`
	Version             string `json:"version"`
	ReservationID       string `json:"reservaId"`
	KernelID            string `json:"nucleoId"`
	OwnerSHA256         string `json:"propietarioSha256"`
	MemberSHA256        string `json:"integranteSha256"`
	Role                string `json:"rol"`
	FencedRevision      string `json:"revisionFrenada"`
	FencedEpoch         string `json:"epocaFrenada"`
	FenceSHA256         string `json:"frenoSha256"`
	AttemptID           string `json:"intentoId"`
	VerifiedStateSHA256 string `json:"estadoVerificadoSha256"`
}

func validFenceReceipt(r *PortalFenceReadReceipt, p *PortalFenceReadInput) bool {
	if r == nil || !validFenceInput(p) || r.Source != "portal-sim-reset-fence" || r.Version != "crea-sim-fence-read-v1" || r.KernelID != p.KernelID ||
		!generationCounter.MatchString(r.FencedRevision) || !generationCounter.MatchString(r.FencedEpoch) || r.FencedRevision == "0" || r.FencedEpoch == "0" {
		return false
	}
	for _, s := range []string{r.ReservationID, r.AttemptID} {
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil || id.String() != s {
			return false
		}
	}
	for _, s := range []string{r.OwnerSHA256, r.MemberSHA256, r.FenceSHA256, r.VerifiedStateSHA256} {
		if !initialSHA.MatchString(s) {
			return false
		}
	}
	switch r.Role {
	case "DIRECTOR", "STRATEGIC_COORDINATION", "OPERATIONAL_COORDINATION", "THERAPIST", "TECHNICAL_DIRECTION", "PRESCHOOL_ASSISTANT", "OPERATIONS_SUPPORT":
		return true
	}
	return false
}

// An exact profile owns exactly one input type; legacy retained records stay
// byte-identical because every new field is omitted when absent.
func validProvenanceInput(profile string, generation *PortalGenerationReadInput, fence *PortalFenceReadInput) bool {
	switch profile {
	case PortalGenerationReadProfile:
		return validGenerationInput(generation) && fence == nil
	case PortalFenceReadProfile:
		return generation == nil && validFenceInput(fence)
	default:
		return generation == nil && fence == nil
	}
}
func provenanceReadBytes(profile string, generation *PortalGenerationReadInput, fence *PortalFenceReadInput) ([]byte, string, error) {
	if !IsProvenanceReadProfile(profile) || !validProvenanceInput(profile, generation, fence) {
		return nil, "", fmt.Errorf("closed provenance input refused")
	}
	if profile == PortalFenceReadProfile {
		b, e := json.Marshal(fence)
		return b, portalFenceReadCommand, e
	}
	b, e := generationReadBytes(generation)
	return b, portalGenerationReadCommand, e
}
func validProvenanceReceipt(profile string, r InitialNativeReceipt, in initialIntent) bool {
	if profile == PortalFenceReadProfile {
		return r.GenerationRead == nil && validFenceReceipt(r.FenceRead, in.FenceRead)
	}
	return r.FenceRead == nil && validGenerationReceipt(r.GenerationRead, in.GenerationRead)
}
func decodeProvenanceOutput(r *InitialNativeReceipt, p IsolatedInitialJobPlan, b []byte) bool {
	if p.Profile == PortalFenceReadProfile {
		out := &PortalFenceReadReceipt{}
		if DecodeInitialBootstrapJSON(b, out) == nil && validFenceReceipt(out, p.FenceRead) {
			r.FenceRead = out
			return true
		}
		return false
	}
	out := &PortalGenerationReadReceipt{}
	if DecodeInitialBootstrapJSON(b, out) == nil && validGenerationReceipt(out, p.GenerationRead) {
		r.GenerationRead = out
		return true
	}
	return false
}
