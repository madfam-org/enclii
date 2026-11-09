package api

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

type karafielStageFrontendPrerequisite struct {
	Origin             string `json:"origin"`
	APIOrigin          string `json:"api_origin"`
	Issuer             string `json:"issuer"`
	ClientIDSHA256     string `json:"client_id_sha256"`
	RegistrationSHA256 string `json:"registration_sha256"`
	CustodySHA256      string `json:"custody_sha256"`
}
type karafielStageBrokerPrerequisite struct {
	Mode                string `json:"mode"`
	Cache               string `json:"cache"`
	ConfigurationSHA256 string `json:"configuration_sha256"`
}
type karafielStagePrerequisites struct {
	Schema              string                                       `json:"schema"`
	Phase               string                                       `json:"phase"`
	ObservedAt          string                                       `json:"observed_at"`
	ServiceID           uuid.UUID                                    `json:"service_id"`
	AppNamespaceUID     string                                       `json:"app_namespace_uid"`
	SourceRevision      string                                       `json:"source_revision"`
	ConfigurationSHA256 string                                       `json:"configuration_sha256"`
	CoreCustodySHA256   string                                       `json:"core_custody_sha256"`
	Broker              karafielStageBrokerPrerequisite              `json:"broker"`
	Frontends           map[string]karafielStageFrontendPrerequisite `json:"frontends"`
	ObservationSHA256   string                                       `json:"observation_sha256"`
}

// Fresh reads only: this does not provision, rotate, reconcile, migrate, issue
// tokens, or accept a caller-supplied readiness Boolean/URL/credential.
func (h *Handler) readKarafielStagePrerequisites(ctx context.Context, serviceID uuid.UUID) (*karafielStagePrerequisites, error) {
	if h.config == nil || h.config.JanuaAdminToken == "" || h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		return nil, errKarafielStage
	}
	source, err := h.karafielPrerequisiteSource(ctx, serviceID)
	if err != nil {
		return nil, errKarafielStage
	}
	return h.readKarafielOwnedStagePrerequisites(ctx, source)
}

func (h *Handler) karafielPrerequisiteSource(ctx context.Context, serviceID uuid.UUID) (*karafielStageSource, error) {
	source, err := h.readKarafielStageSourceMode(ctx, serviceID, false, nil, true)
	if err != nil {
		if h.repos == nil || h.repos.DatabaseAddons == nil || h.repos.Projects == nil {
			return nil, errKarafielStage
		}
		rows, e := h.repos.DatabaseAddons.ListByProject(ctx, mustStageProject(h))
		if e != nil {
			return nil, errKarafielStage
		}
		for _, a := range rows {
			if a != nil && a.Name == addons.KarafielStagingAddon {
				source, _, _, err = h.karafielOwnedAddon(ctx, serviceID, a.ID)
				break
			}
		}
	}
	if err != nil || source == nil {
		return nil, errKarafielStage
	}
	return source, nil
}

func (h *Handler) readKarafielOwnedStagePrerequisites(ctx context.Context, source *karafielStageSource) (*karafielStagePrerequisites, error) {
	stage, err := h.vaultClient.GetSecretData(ctx, "secret/karafiel-staging")
	if err != nil {
		return nil, errKarafielStage
	}
	production, err := h.vaultClient.GetSecretData(ctx, "secret/karafiel")
	if err != nil {
		return nil, errKarafielStage
	}
	targets, err := secretsintake.LoadRegistry()
	if err != nil {
		return nil, errKarafielStage
	}
	coreOrigin, err := qualifiedKarafielIntakeOrigin(stage, targets["karafiel-staging/core"])
	if err != nil {
		return nil, errKarafielStage
	}
	sessionOrigin, err := qualifiedKarafielIntakeOrigin(stage, targets["karafiel-staging/admin-session"])
	if err != nil {
		return nil, errKarafielStage
	}
	custody, err := h.readKarafielStageApplicationCustody(ctx, stage, production)
	if err != nil {
		return nil, errKarafielStage
	}
	frontendConfig, err := h.readKarafielStageFrontendConfiguration(ctx)
	if err != nil {
		return nil, errKarafielStage
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	issuerHash, err := readKarafielJanuaIssuer(ctx, client)
	if err != nil {
		return nil, errKarafielStage
	}
	frontends := map[string]karafielStageFrontendPrerequisite{}
	for _, role := range []string{"web", "admin"} {
		id := stage[role+"_oidc_client_id"].(string) // custody validates fixed field types first.
		registration, err := readKarafielJanuaRegistration(ctx, client, h.config.JanuaAdminToken, role, id)
		if err != nil {
			return nil, errKarafielStage
		}
		origin := "https://staging-app.karafiel.mx"
		if role == "admin" {
			origin = "https://staging-admin.karafiel.mx"
		}
		frontends[role] = karafielStageFrontendPrerequisite{origin, "https://staging-api.karafiel.mx", "https://auth.madfam.io", karafielMaterialHash([]byte(id)), stageJSONHash([]string{issuerHash, registration}), custody[role]}
	}
	if frontends["web"].ClientIDSHA256 == frontends["admin"].ClientIDSHA256 {
		return nil, errKarafielStage
	}
	descriptor, ok := karafielStageDescriptor()
	if !ok || descriptor.Config["CELERY_BROKER_URL"] != "memory://" || descriptor.Config["CELERY_RESULT_BACKEND"] != "cache+memory://" || descriptor.Config["JANUA_AUDIENCE"] != "karafiel-staging-api" || descriptor.Config["JANUA_ADDITIONAL_AUDIENCES"] != "" {
		return nil, errKarafielStage
	}
	// Cross-system reads are not one transaction. Refuse a changed source,
	// projection, or Vault group rather than minting a mixed-time observation.
	currentStage, e := h.vaultClient.GetSecretData(ctx, "secret/karafiel-staging")
	if e != nil || !reflect.DeepEqual(stage, currentStage) {
		return nil, errKarafielStage
	}
	currentCustody, e := h.readKarafielStageApplicationCustody(ctx, currentStage, production)
	if e != nil || !reflect.DeepEqual(custody, currentCustody) {
		return nil, errKarafielStage
	}
	currentFrontend, e := h.readKarafielStageFrontendConfiguration(ctx)
	if e != nil || currentFrontend != frontendConfig {
		return nil, errKarafielStage
	}
	currentSource, e := h.karafielPrerequisiteSource(ctx, source.Service.ID)
	if e != nil || currentSource.Observation.ObservationSHA256 != source.Observation.ObservationSHA256 {
		return nil, errKarafielStage
	}
	// Stable custody excludes routine ESO refresh/resource versions. Secret UIDs,
	// exact specs and actual value digests remain bound through the entire soak.
	out := &karafielStagePrerequisites{Schema: "enclii-karafiel-staging-prerequisites-v1", Phase: "PRE_BUILD_APPLICATION_READY", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ServiceID: source.Service.ID, AppNamespaceUID: string(source.Namespace.UID), SourceRevision: source.Observation.SourceRevision, ConfigurationSHA256: stageJSONHash([]string{stageJSONHash(descriptor), frontendConfig}), CoreCustodySHA256: stageJSONHash([]string{coreOrigin, sessionOrigin, custody["core"], custody["session"]}), Broker: karafielStageBrokerPrerequisite{"memory", "locmem", stageJSONHash(map[string]string{"broker": "memory://", "result": "cache+memory://", "cache": "django-default-locmem"})}, Frontends: frontends}
	out.ObservationSHA256 = stageJSONHash(*out)
	return out, nil
}

func decodeKarafielPrereqJSON(raw []byte, out any) error {
	return k8s.DecodeInitialBootstrapJSON(raw, out)
}
