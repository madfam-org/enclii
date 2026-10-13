// Package workloadtargets describes explicit operational targets. Observation
// selectors and service-name suffixes cannot create a mutation binding.
package workloadtargets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

var (
	ErrTrackingMethod = errors.New("configured Argo tracking method missing or unsupported")
	ErrIntent         = errors.New("persisted workload ownership intent unavailable or conflicting")
	ErrOwnership      = errors.New("actual workload ownership/source could not be verified")
	ErrConflict       = errors.New("explicit workload binding conflict")
	ErrCommitUnknown  = errors.New("target transaction outcome unknown; read the same idempotency ID")
	ErrUnavailable    = errors.New("explicit workload binding store unavailable")
	sha40             = regexp.MustCompile(`^[a-f0-9]{40}$`)
	sha64             = regexp.MustCompile(`^[a-f0-9]{64}$`)
	uid               = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	repoPattern       = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_-]+)/([A-Za-z0-9_.-]+)$`)
	refPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
	pathSegment       = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	imagePattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]+@sha256:[a-f0-9]{64}$`)
)

type Identity struct {
	ProjectID           uuid.UUID `json:"project_id"`
	ServiceID           uuid.UUID `json:"service_id"`
	EnvironmentID       uuid.UUID `json:"environment_id"`
	RegistrationID      uuid.UUID `json:"registration_id"`
	Namespace           string    `json:"namespace"`
	NamespaceUID        string    `json:"namespace_uid"`
	Deployment          string    `json:"deployment"`
	DeploymentUID       string    `json:"deployment_uid"`
	Application         string    `json:"application"`
	ApplicationUID      string    `json:"application_uid"`
	ArgoConfigUID       string    `json:"argo_config_uid"`
	TrackingMethod      string    `json:"tracking_method"`
	InstallationID      string    `json:"installation_id"`
	Repo                string    `json:"repo"`
	Path                string    `json:"path"`
	Ref                 string    `json:"ref"`
	Revision            string    `json:"revision"`
	DeclaredIdentitySHA string    `json:"declared_identity_sha256"`
}

func CanonicalRepo(raw string) (string, string, error) {
	raw = strings.TrimSuffix(raw, ".git")
	parts := repoPattern.FindStringSubmatch(raw)
	if len(parts) != 3 || parts[2] == "." || parts[2] == ".." {
		return "", "", ErrIntent
	}
	return raw, parts[1] + "/" + parts[2], nil
}

func (i Identity) Validate() error {
	if i.ProjectID == uuid.Nil || i.ServiceID == uuid.Nil || i.EnvironmentID == uuid.Nil || i.RegistrationID == uuid.Nil ||
		len(validation.IsDNS1123Label(i.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(i.Deployment)) != 0 ||
		len(validation.IsDNS1123Subdomain(i.Application)) != 0 || !uid.MatchString(i.NamespaceUID) ||
		!uid.MatchString(i.DeploymentUID) || !uid.MatchString(i.ApplicationUID) || !uid.MatchString(i.ArgoConfigUID) ||
		(i.TrackingMethod != "annotation" && i.TrackingMethod != "annotation+label") || len(i.InstallationID) > 128 ||
		(i.InstallationID != "" && !uid.MatchString(i.InstallationID)) || !sha40.MatchString(i.Revision) ||
		!sha64.MatchString(i.DeclaredIdentitySHA) || !ValidPath(i.Path) || !refPattern.MatchString(i.Ref) {
		return ErrIntent
	}
	canonical, _, err := CanonicalRepo(i.Repo)
	if err != nil || canonical != i.Repo {
		return ErrIntent
	}
	return nil
}

func ValidPath(value string) bool {
	if value == "" || len(value) > 256 || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || !pathSegment.MatchString(segment) {
			return false
		}
	}
	return true
}

func ValidRevision(value string) bool { return sha40.MatchString(value) }

type Binding struct {
	ID       uuid.UUID        `json:"id"`
	Identity Identity         `json:"identity"`
	PlanSHA  string           `json:"plan_sha256"`
	Versions ReviewedVersions `json:"reviewed_resource_versions"`
}

// SameOwner permits a normal new revision in the same declared target. It does
// not grant image promotion or simulator-generation enrollment/upgrade.
func SameOwner(a, b Identity) bool {
	a.Revision, b.Revision = "", ""
	return a == b
}

func (b Binding) Validate() error {
	if b.ID == uuid.Nil || !sha64.MatchString(b.PlanSHA) || !b.Versions.valid() {
		return ErrIntent
	}
	return b.Identity.Validate()
}

type ReviewedVersions struct {
	Namespace   string `json:"namespace"`
	Application string `json:"application"`
	Deployment  string `json:"deployment"`
	ArgoConfig  string `json:"argo_config"`
}

func (v ReviewedVersions) valid() bool {
	for _, value := range []string{v.Namespace, v.Application, v.Deployment, v.ArgoConfig} {
		if !uid.MatchString(value) {
			return false
		}
	}
	return true
}

type Plan struct {
	Version      string           `json:"version"`
	Identity     Identity         `json:"identity"`
	Versions     ReviewedVersions `json:"reviewed_resource_versions"`
	Incumbent    *Binding         `json:"incumbent"`
	ActualImages []string         `json:"actual_images"`
	SHA          string           `json:"plan_sha256"`
}

func hash(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", ErrIntent
	}
	var canonical interface{}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if decoder.Decode(&canonical) != nil {
		return "", ErrIntent
	}
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", ErrIntent
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (p Plan) Fingerprint() (string, error) {
	p.SHA = ""
	return hash(p)
}
