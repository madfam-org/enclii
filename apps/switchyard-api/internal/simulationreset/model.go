// Package simulationreset records declared reset intent. It does not supply
// verified admission, producer, owner-job or lifecycle authority.
package simulationreset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalid           = errors.New("invalid reset metadata")
	ErrConflict          = errors.New("reset metadata conflict")
	ErrBarrierUnverified = errors.New("reset admission barrier unverified")
	ErrHeld              = errors.New("training lifecycle held")
	ErrIdentityDrift     = errors.New("training enrollment identity changed")
	sha64                = regexp.MustCompile(`^[a-f0-9]{64}$`)
	sha40                = regexp.MustCompile(`^[a-f0-9]{40}$`)
	decimal              = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})$`)
	positiveDecimal      = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
	identifier           = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	database             = regexp.MustCompile(`^[a-z][a-z0-9_]{0,58}_sim$`)
	uidPattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	imagePattern         = regexp.MustCompile(`^ghcr\.io/madfam-org/crea-map/crea-map-web@sha256:[a-f0-9]{64}$`)
)

type Role string

var roles = [...]Role{"DIRECTOR", "STRATEGIC_COORDINATION", "OPERATIONAL_COORDINATION", "THERAPIST", "TECHNICAL_DIRECTION", "PRESCHOOL_ASSISTANT", "OPERATIONS_SUPPORT"}

// Enrollment is an immutable expected identity, not a signed-image verification
// receipt. Actual source/witness/admission verification is a later closed seam.
type Enrollment struct {
	ID, ProjectID, ServiceID, AddonID                                            uuid.UUID
	KernelID, Namespace, NamespaceUID, DeploymentName, DeploymentUID, ServiceUID string
	AddonNamespace, AddonNamespaceUID, ClusterUID                                string
	ApplicationName, ApplicationUID, ApplicationNamespace                        string
	ApplicationRepo, ApplicationPath, ApplicationRef                             string
	DatabaseName, Image, SourceSHA, BuildRunID, PinSHA                           string
	OriginSHA, CodeSHA, MigrationsSHA, StructureSHA, ManifestSHA                 string
	Templates                                                                    map[Role]uuid.UUID
	CreatedAt                                                                    time.Time
}

func (e Enrollment) Validate() error {
	if e.ID == uuid.Nil || e.ProjectID == uuid.Nil || e.ServiceID == uuid.Nil || e.AddonID == uuid.Nil ||
		!identifier.MatchString(e.KernelID) || !identifier.MatchString(e.Namespace) ||
		e.DeploymentName != e.KernelID || e.AddonNamespace != "project-"+e.ProjectID.String()[:8] ||
		!identifier.MatchString(e.ApplicationName) || e.ApplicationNamespace != "argocd" ||
		e.ApplicationRepo != "https://github.com/madfam-org/crea-sim.git" ||
		e.ApplicationPath != "infra/k8s/production" || e.ApplicationRef != "main" ||
		!database.MatchString(e.DatabaseName) || !imagePattern.MatchString(e.Image) ||
		!sha40.MatchString(e.SourceSHA) || !sha40.MatchString(e.PinSHA) || !positiveDecimal.MatchString(e.BuildRunID) {
		return ErrInvalid
	}
	for _, s := range []string{e.NamespaceUID, e.DeploymentUID, e.ServiceUID, e.AddonNamespaceUID, e.ClusterUID, e.ApplicationUID} {
		if !uidPattern.MatchString(s) {
			return ErrInvalid
		}
	}
	for _, s := range []string{e.OriginSHA, e.CodeSHA, e.MigrationsSHA, e.StructureSHA, e.ManifestSHA} {
		if !sha64.MatchString(s) {
			return ErrInvalid
		}
	}
	if len(e.Templates) != len(roles) {
		return ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, role := range roles {
		id := e.Templates[role]
		if id == uuid.Nil || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}

// Fence keeps owner only as a hash. Member is the canonical synthetic template
// derived from the immutable enrollment; it is not returned by a reader Job.
type Fence struct {
	ReservationID        uuid.UUID
	OwnerSHA             string
	Role                 Role
	Revision, Epoch, SHA string
}

func (f Fence) Validate(e Enrollment) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if f.ReservationID == uuid.Nil || !sha64.MatchString(f.OwnerSHA) || !decimal.MatchString(f.Revision) || !decimal.MatchString(f.Epoch) {
		return ErrInvalid
	}
	expected, err := FenceSHA(e, f)
	if err != nil || f.SHA != expected {
		return ErrInvalid
	}
	return nil
}

// FenceSHA uses only validated ASCII values and encoding/json's sorted map keys,
// matching the accepted Node compact-JSON nine-field fence contract exactly.
func FenceSHA(e Enrollment, f Fence) (string, error) {
	member := e.Templates[f.Role]
	if member == uuid.Nil || !decimal.MatchString(f.Revision) || !decimal.MatchString(f.Epoch) ||
		!database.MatchString(e.DatabaseName) || !identifier.MatchString(e.KernelID) || f.ReservationID == uuid.Nil || !sha64.MatchString(f.OwnerSHA) {
		return "", ErrInvalid
	}
	bytes, err := json.Marshal(map[string]string{"version": "map-reset-fence-v1", "baseEsperada": e.DatabaseName,
		"nucleoId": e.KernelID, "ejercicioId": f.ReservationID.String(), "propietarioSha256": f.OwnerSHA,
		"integranteId": member.String(), "rol": string(f.Role), "revisionFrenada": f.Revision, "epocaFrenada": f.Epoch})
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

type Phase string

const (
	BarrierUnverified Phase = "BARRIER_UNVERIFIED"
	Held              Phase = "HELD"
	RestoringHeld     Phase = "RESTORING_HELD"
	RestoredHeld      Phase = "RESTORED_HELD"
	Releasing         Phase = "RELEASING"
	Uncertain         Phase = "INCIERTO"
)

type Hold struct {
	EnrollmentID, OperationID uuid.UUID
	Fence                     Fence
	Phase                     Phase
	Version                   int64
	DeletedAt                 *time.Time
}

func (h Hold) Validate(e Enrollment) error {
	if h.EnrollmentID != e.ID || h.OperationID == uuid.Nil || h.Version < 0 || h.DeletedAt != nil {
		return ErrInvalid
	}
	if err := h.Fence.Validate(e); err != nil {
		return err
	}
	switch h.Phase {
	case BarrierUnverified, Held, RestoringHeld, RestoredHeld, Releasing, Uncertain:
		return nil
	default:
		return ErrInvalid
	}
}

// BarrierProof has no production constructor. Caller flags, stored hashes or a
// synthetic Phase cannot mint authority. A future trusted verifier is required.
type BarrierProof struct {
	enrollmentID, operationID uuid.UUID
	deploymentUID, fenceSHA   string
}

func RequireBarrier(e Enrollment, h Hold, p *BarrierProof) error {
	if e.Validate() != nil || h.Validate(e) != nil {
		return ErrInvalid
	}
	if p == nil || p.enrollmentID != e.ID || p.operationID != h.OperationID || p.deploymentUID != e.DeploymentUID || p.fenceSHA != h.Fence.SHA {
		return ErrBarrierUnverified
	}
	return nil
}

func lockName(e Enrollment) string {
	return fmt.Sprintf("enclii.sim-reset:%s/%s", e.ProjectID, e.ServiceID)
}
