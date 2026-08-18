package addons

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// identifierRe validates schema / role names before they reach SQL. PostgREST
// roles and schemas are simple identifiers; anything else is rejected at the
// service boundary (defense in depth on top of SQL quoting).
var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// DataAPIService is the business logic for enabling / disabling / inspecting an
// addon's auto-generated REST API (PostgREST), and for minting JWTs signed with
// the addon's data-API secret. See docs/architecture/data-api-postgrest.md.
type DataAPIService struct {
	repos       *db.Repositories
	k8sClient   *k8s.Client
	logger      *logrus.Logger
	provisioner *DataAPIProvisioner
}

// NewDataAPIService constructs a data-API service. baseDomain (e.g.
// "data.enclii.dev") is where public data-API hosts live.
func NewDataAPIService(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger, baseDomain string) *DataAPIService {
	return &DataAPIService{
		repos:       repos,
		k8sClient:   k8sClient,
		logger:      logger,
		provisioner: NewDataAPIProvisioner(k8sClient, logger, baseDomain),
	}
}

// EnableDataAPIRequest carries the inputs to enable an addon's data-API.
type EnableDataAPIRequest struct {
	AddonID  uuid.UUID
	Schemas  string // default "public"
	AnonRole string // default "anon"
	Actor    EventActor
}

// EnableDataAPI turns on the auto-generated REST API for a Postgres addon. It
// generates a JWT signing secret (stored only in a K8s Secret), writes the
// managed_db_data_apis row in pending, and emits an audit event. The
// DataAPIReconciler picks it up and provisions the PostgREST workload.
//
// Idempotent-ish: re-enabling a disabled data-API reuses the row and resets it
// to pending; re-enabling a ready one is a no-op that returns the current row.
func (s *DataAPIService) EnableDataAPI(ctx context.Context, req EnableDataAPIRequest) (*types.DataAPI, error) {
	logger := s.logger.WithField("addon_id", req.AddonID)

	addon, err := s.repos.DatabaseAddons.GetByID(ctx, req.AddonID)
	if err != nil {
		return nil, fmt.Errorf("addon not found: %w", err)
	}

	// Data-API is Postgres-only (PostgREST is Postgres-only by construction).
	if addon.Type != types.DatabaseAddonTypePostgres {
		return nil, fmt.Errorf("data-API is only supported for postgres addons, not %s", addon.Type)
	}
	if addon.Status != types.DatabaseAddonStatusReady {
		return nil, fmt.Errorf("addon is not ready (status: %s); cannot enable data-API", addon.Status)
	}

	// Normalize + validate schemas and anon role.
	schemas := normalizeSchemas(req.Schemas)
	if err := validateSchemas(schemas); err != nil {
		return nil, err
	}
	anon := strings.TrimSpace(req.AnonRole)
	if anon == "" {
		anon = DataAPIRoleAnon
	}
	if !identifierRe.MatchString(anon) {
		return nil, fmt.Errorf("invalid anon role %q", anon)
	}

	// If a row already exists and is ready/provisioning, return it (no churn).
	if existing, err := s.repos.DataAPIs.GetByAddon(ctx, req.AddonID); err == nil {
		switch existing.Status {
		case types.DataAPIStatusReady, types.DataAPIStatusProvisioning, types.DataAPIStatusPending:
			return existing, nil
		}
	}

	resourceName := DataAPIResourceName(addon)
	jwtSecretName := resourceName + dataAPIJWTSecretSuffix

	// Generate + store the JWT signing secret. It never leaves the cluster and
	// is never persisted in the DB row (only its Secret name is).
	jwtSecret, err := GenerateSecretValue(32)
	if err != nil {
		return nil, err
	}
	if err := s.provisioner.ensureSecret(ctx, addon, jwtSecretName, map[string][]byte{
		dataAPIJWTSecretKey: []byte(jwtSecret),
	}); err != nil {
		return nil, fmt.Errorf("store data-API JWT secret: %w", err)
	}

	// Bound the PostgREST pool by the plan's max_connections (leave headroom).
	pool := 10
	if plan, err := s.repos.ManagedDBPlans.GetByCode(ctx, addon.Plan); err == nil && plan.MaxConnections > 2 {
		if plan.MaxConnections-2 < pool {
			pool = plan.MaxConnections - 2
		}
	}

	api := &types.DataAPI{
		AddonID:         addon.ID,
		ProjectID:       addon.ProjectID,
		Status:          types.DataAPIStatusPending,
		StatusMessage:   "Data-API enable requested",
		Schemas:         strings.Join(schemas, ","),
		AnonRole:        anon,
		DBPool:          pool,
		JWTSecretName:   jwtSecretName,
		Host:            s.provisioner.DataAPIHost(addon),
		K8sResourceName: resourceName,
		EnabledAt:       ptrTime(time.Now()),
	}

	if err := s.repos.DataAPIs.Upsert(ctx, api); err != nil {
		return nil, fmt.Errorf("persist data-API row: %w", err)
	}

	s.emitEvent(ctx, addon, req.Actor, db.EventAddonDataAPIEnabled, map[string]interface{}{
		"schemas":   api.Schemas,
		"anon_role": api.AnonRole,
		"host":      api.Host,
	})

	logger.WithField("host", api.Host).Info("Data-API enable requested")
	return api, nil
}

// DisableDataAPI flips the data-API to disabling; the reconciler tears down the
// K8s objects and marks it disabled. Emits an audit event.
func (s *DataAPIService) DisableDataAPI(ctx context.Context, addonID uuid.UUID, actor EventActor) error {
	logger := s.logger.WithField("addon_id", addonID)

	api, err := s.repos.DataAPIs.GetByAddon(ctx, addonID)
	if err != nil {
		return fmt.Errorf("data-API not found: %w", err)
	}
	if api.Status == types.DataAPIStatusDisabled {
		return nil // already disabled
	}

	if err := s.repos.DataAPIs.UpdateStatus(ctx, addonID, types.DataAPIStatusDisabling, "Data-API disable requested"); err != nil {
		return fmt.Errorf("mark data-API disabling: %w", err)
	}

	addon, err := s.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err == nil {
		s.emitEvent(ctx, addon, actor, db.EventAddonDataAPIDisabled, map[string]interface{}{
			"host": api.Host,
		})
	}

	logger.Info("Data-API disable requested")
	return nil
}

// GetDataAPI returns the data-API row for an addon (sql.ErrNoRows if none).
func (s *DataAPIService) GetDataAPI(ctx context.Context, addonID uuid.UUID) (*types.DataAPI, error) {
	return s.repos.DataAPIs.GetByAddon(ctx, addonID)
}

// MintToken signs a short-lived HS256 JWT with the addon's data-API signing
// secret (read from the K8s Secret — it never leaves the cluster). The token's
// `role` claim selects the Postgres role PostgREST switches to; RLS in the
// tenant DB is the authorization boundary.
func (s *DataAPIService) MintToken(ctx context.Context, addonID uuid.UUID, req types.DataAPITokenRequest) (*types.DataAPITokenResponse, error) {
	api, err := s.repos.DataAPIs.GetByAddon(ctx, addonID)
	if err != nil {
		return nil, fmt.Errorf("data-API not found: %w", err)
	}
	if api.Status == types.DataAPIStatusDisabled {
		return nil, fmt.Errorf("data-API is disabled")
	}

	addon, err := s.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err != nil {
		return nil, fmt.Errorf("addon not found: %w", err)
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = DataAPIRoleAuthenticated
	}
	if !identifierRe.MatchString(role) {
		return nil, fmt.Errorf("invalid role %q", role)
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	if ttl > 24*time.Hour {
		ttl = 24 * time.Hour
	}

	secret, err := s.readJWTSecret(ctx, addon, api)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	exp := now.Add(ttl)

	claims := jwt.MapClaims{
		"role": role,
		"iat":  now.Unix(),
		"exp":  exp.Unix(),
	}
	// Extra tenant-supplied claims (e.g. a user id RLS keys on). Reserved claims
	// cannot be overridden.
	for k, v := range req.Claims {
		switch k {
		case "role", "iat", "exp":
			continue
		default:
			claims[k] = v
		}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}

	return &types.DataAPITokenResponse{
		Token:     signed,
		Role:      role,
		ExpiresAt: exp,
	}, nil
}

// readJWTSecret pulls the signing secret out of the addon's K8s Secret.
func (s *DataAPIService) readJWTSecret(ctx context.Context, addon *types.DatabaseAddon, api *types.DataAPI) (string, error) {
	name := api.JWTSecretName
	if name == "" {
		name = DataAPIResourceName(addon) + dataAPIJWTSecretSuffix
	}
	secret, err := s.k8sClient.Kube().CoreV1().Secrets(addon.K8sNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return "", fmt.Errorf("data-API JWT secret not found (data-API may not be provisioned yet)")
		}
		return "", err
	}
	val := string(secret.Data[dataAPIJWTSecretKey])
	if val == "" {
		return "", fmt.Errorf("data-API JWT secret is empty")
	}
	return val, nil
}

// emitEvent writes to the shared addon event ledger, reusing the addon service's
// no-fail-on-ledger-outage posture.
func (s *DataAPIService) emitEvent(ctx context.Context, addon *types.DatabaseAddon, actor EventActor, eventType db.ManagedDBAddonEventType, details map[string]interface{}) {
	if s.repos == nil || s.repos.ManagedDBAddonEvents == nil {
		return
	}
	_, err := s.repos.ManagedDBAddonEvents.Insert(ctx, db.InsertEventParams{
		AddonID:        addon.ID,
		ProjectID:      addon.ProjectID,
		EventType:      eventType,
		ActorUserSub:   actor.UserSub,
		ActorUserEmail: actor.UserEmail,
		Details:        details,
	})
	if err != nil {
		s.logger.WithError(err).WithFields(logrus.Fields{
			"addon_id":   addon.ID,
			"event_type": eventType,
		}).Warn("Failed to write data-API event")
	}
}

// normalizeSchemas splits a comma-separated schema list, trims, and drops empties.
// Defaults to ["public"] when the input is empty.
func normalizeSchemas(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{DataAPIDefaultSchemas}
	}
	return out
}

// validateSchemas rejects any schema name that is not a plain SQL identifier.
func validateSchemas(schemas []string) error {
	for _, sch := range schemas {
		if !identifierRe.MatchString(sch) {
			return fmt.Errorf("invalid schema name %q", sch)
		}
	}
	return nil
}

func ptrTime(t time.Time) *time.Time { return &t }
