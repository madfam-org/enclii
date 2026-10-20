package k8s

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Public source hashes, never credentials. Changing the reader needs review in
// this fixed profile as well as qualifying the new signed producer image.
const PortalGenerationReaderSHA256 = "24a9008bd35f1beadde62cafdd50795b77c267f91f68307988a474b1a3c540f1" // pragma: allowlist secret
const PortalGenerationSchemaSHA256 = "66b3ab6bc4306efcb75ee75cdad85084ac1c6b9a6fd4542df99c0a7aafdf8496" // pragma: allowlist secret

// No caller bytes are interpolated into a command. The immutable, Job-owned
// Secret supplies only the closed request as stdin through this fixed path.
const portalGenerationReadCommand = "exec node /app/packages/entrenamiento/src/leer-generacion-retenida.ts < /var/run/crea-sim/request/request.json"

type PortalGenerationReadInput struct {
	Version       string `json:"version"`
	ReservationID string `json:"reservaId"`
	KernelID      string `json:"nucleoId"`
}

func validGenerationInput(p *PortalGenerationReadInput) bool {
	if p == nil || p.Version != "crea-sim-generation-read-v1" {
		return false
	}
	id, err := uuid.Parse(p.ReservationID)
	return err == nil && id != uuid.Nil && id.String() == p.ReservationID &&
		(p.KernelID == "crea-sim-map01" || p.KernelID == "crea-sim-map02" || p.KernelID == "crea-sim-map03")
}

// ValidatePortalGenerationReadInput closes identifiers before custody access.
func ValidatePortalGenerationReadInput(p *PortalGenerationReadInput) error {
	if !validGenerationInput(p) {
		return fmt.Errorf("closed provenance read input refused")
	}
	return nil
}

func generationReadBytes(p *PortalGenerationReadInput) ([]byte, error) {
	if err := ValidatePortalGenerationReadInput(p); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

type PortalGenerationReadReceipt struct {
	Source                 string `json:"fuente"`
	Version                string `json:"version"`
	ReservationID          string `json:"reservaId"`
	KernelID               string `json:"nucleoId"`
	Database               string `json:"baseEsperada"`
	OwnerSHA256            string `json:"propietarioSha256"`
	MemberSHA256           string `json:"integranteSha256"`
	Role                   string `json:"rol"`
	Scenario               string `json:"escenario"`
	ObservedPhase          string `json:"faseObservada"`
	EmptyRevision          string `json:"revisionVacia"`
	EmptyEpoch             string `json:"epocaVacia"`
	ManifestSHA256         string `json:"manifiestoSha256"`
	SeedSHA256             string `json:"huellaSemilla"`
	CodeSHA256             string `json:"codigoSha256"`
	FixtureSHA256          string `json:"fixtureSha256"`
	InitialTime            string `json:"ahoraInicial"`
	SchemaSHA256           string `json:"schemaSha256"`
	DatabaseIdentitySHA256 string `json:"databaseIdentitySha256"`
	AdoptionAuthorized     *bool  `json:"adoptionAuthorized"`
}

var generationCounter = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})$`)
var generationScenario = regexp.MustCompile(`^[a-z][a-z0-9-]{0,79}$`)

func validGenerationReceipt(r *PortalGenerationReadReceipt, p *PortalGenerationReadInput) bool {
	if r == nil || !validGenerationInput(p) || r.Source != "portal-sim-retained-generation" || r.Version != p.Version || r.ReservationID != p.ReservationID || r.KernelID != p.KernelID || r.AdoptionAuthorized == nil || *r.AdoptionAuthorized {
		return false
	}
	wantDB := map[string]string{"crea-sim-map01": "crea_sim_kernel01_sim", "crea-sim-map02": "crea_sim_kernel02_sim", "crea-sim-map03": "crea_sim_kernel03_sim"}[p.KernelID]
	if r.Database != wantDB || r.SchemaSHA256 != PortalGenerationSchemaSHA256 || !generationCounter.MatchString(r.EmptyRevision) || !generationCounter.MatchString(r.EmptyEpoch) || !generationScenario.MatchString(r.Scenario) {
		return false
	}
	for _, s := range []string{r.OwnerSHA256, r.MemberSHA256, r.ManifestSHA256, r.SeedSHA256, r.CodeSHA256, r.FixtureSHA256, r.SchemaSHA256, r.DatabaseIdentitySHA256} {
		if !initialSHA.MatchString(s) {
			return false
		}
	}
	roles := map[string]bool{"DIRECTOR": true, "STRATEGIC_COORDINATION": true, "OPERATIONAL_COORDINATION": true, "THERAPIST": true, "TECHNICAL_DIRECTION": true, "PRESCHOOL_ASSISTANT": true, "OPERATIONS_SUPPORT": true}
	phases := map[string]bool{"RESERVADA": true, "ACTIVA": true, "INCIERTA": true, "RESTABLECIENDO": true, "CERRADA": true}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", r.InitialTime)
	return roles[r.Role] && phases[r.ObservedPhase] && err == nil && t.Format("2006-01-02T15:04:05.000Z") == r.InitialTime
}

func expectedOwnerRole(profile string) string {
	if IsProvenanceReadProfile(profile) {
		return ""
	}
	return "enclii_migrator"
}

// ProvenancePlanSHA256 binds the fixed spec, input, operation key and verified
// public identities. It discloses neither URI nor credential material.
func (p IsolatedInitialJobPlan) ProvenancePlanSHA256() (string, error) {
	if !IsProvenanceReadProfile(p.Profile) {
		return "", fmt.Errorf("provenance plan profile refused")
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
