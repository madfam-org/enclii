// Package januastage describes the closed, inert Janua staging identity.
// It is deliberately outside services and native043 activation consumers.
package januastage

import (
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const Profile = "janua-staging-inert-v1"
const Namespace = "janua-staging"
const Deployment = "janua-api-staging"
const Application = "janua-staging"
const Repository = "https://github.com/madfam-org/janua.git"
const ManifestPath = "k8s/overlays/staging"

var ErrRefused = errors.New("closed inert Janua staging authority unavailable")
var hash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var revision = regexp.MustCompile(`^[a-f0-9]{40}$`)

// Authority is a retained native-object receipt, never a runtime readiness or
// migration/soak approval. Consumers must obtain it from a fresh native observer;
// caller JSON and ValidateAuthorityShape cannot establish live ownership.
type Authority struct {
	ID                       uuid.UUID `json:"id"`
	ProjectID                uuid.UUID `json:"project_id"`
	EnvironmentID            uuid.UUID `json:"environment_id"`
	RepositoryRegistrationID uuid.UUID `json:"repository_registration_id"`
	HeldIntentID             uuid.UUID `json:"held_intent_id"`
	HeldServiceID            uuid.UUID `json:"held_service_id"`
	Profile                  string    `json:"profile"`
	NamespaceUID             string    `json:"namespace_uid"`
	DeploymentUID            string    `json:"deployment_uid"`
	APIServiceUID            string    `json:"api_service_uid"`
	RedisDeploymentUID       string    `json:"redis_deployment_uid"`
	RedisServiceUID          string    `json:"redis_service_uid"`
	ApplicationUID           string    `json:"application_uid"`
	AppProjectUID            string    `json:"app_project_uid"`
	SourceRevision           string    `json:"source_revision"`
	SourceSHA256             string    `json:"source_sha256"`
	PlanSHA256               string    `json:"plan_sha256"`
	ActivationHeld           bool      `json:"activation_held"`
	ActiveServiceRegistered  bool      `json:"active_service_registered"`
	Native043BindingProven   bool      `json:"native_043_binding_proven"`
	ObservedAt               string    `json:"observed_at"`
}

func HeldServiceID(project, intent uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(project, []byte(Profile+":"+intent.String()))
}

// ValidateAuthorityShape checks retained data only. The native observer must
// independently compare every actual UID/spec/source and zero-replica hold.
func ValidateAuthorityShape(a Authority) error {
	if a.ID == uuid.Nil || a.ProjectID == uuid.Nil || a.EnvironmentID == uuid.Nil ||
		a.RepositoryRegistrationID == uuid.Nil || a.HeldIntentID == uuid.Nil ||
		a.HeldServiceID != HeldServiceID(a.ProjectID, a.HeldIntentID) ||
		a.Profile != Profile || !a.ActivationHeld || a.ActiveServiceRegistered ||
		a.Native043BindingProven || !revision.MatchString(a.SourceRevision) ||
		!hash.MatchString(a.SourceSHA256) || !hash.MatchString(a.PlanSHA256) {
		return ErrRefused
	}
	seen := map[string]bool{}
	for _, uid := range []string{a.NamespaceUID, a.DeploymentUID, a.APIServiceUID,
		a.RedisDeploymentUID, a.RedisServiceUID, a.ApplicationUID, a.AppProjectUID} {
		if uid == "" || seen[uid] {
			return ErrRefused
		}
		seen[uid] = true
	}
	t, err := time.Parse(time.RFC3339Nano, a.ObservedAt)
	if err != nil || t.IsZero() || t.UTC().Format(time.RFC3339Nano) != a.ObservedAt {
		return ErrRefused
	}
	return nil
}
