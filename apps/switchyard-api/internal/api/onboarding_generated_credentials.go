package api

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Generated onboarding credentials.
//
// `enclii onboard` / `onboard ensure` can ask Switchyard to generate the
// database owner password, a runtime app role's password, and arbitrary
// project-Secret values. Every generated value is drawn here, written straight
// into the project Secret (and, for roles, into Postgres as a SCRAM verifier
// and into the PgBouncer userlist), and is never returned or logged. Responses
// and logs carry key names and actions only.
//
// Re-runs converge without rotating: an existing role whose URL is already in
// the Secret is kept, and an existing generated key is kept. Rotation is an
// explicit request flag. A role that exists while its Secret key does not is a
// visible failure, never an implicit rotation.

const (
	defaultOwnerURLSecretKey   = "DATABASE_URL"     // #nosec G101 -- Secret key name, not a value // pragma: allowlist secret
	defaultAppRoleURLSecretKey = "APP_DATABASE_URL" // #nosec G101 -- Secret key name, not a value // pragma: allowlist secret
)

// Credential actions reported per item.
const (
	credentialCreated = "created"
	credentialKept    = "kept"
	credentialRotated = "rotated"
)

// credentialPostgres is the part of the Postgres provisioner these steps use.
type credentialPostgres interface {
	Provision(ctx context.Context, spec *types.PostgresProvisionSpec) error
	ProvisionGeneratedOwner(ctx context.Context, spec *types.PostgresProvisionSpec, password string) error
	RoleExists(ctx context.Context, roleName string) (bool, error)
	EnsureAppRole(ctx context.Context, role provisioning.AppRole, password string) (provisioning.AppRoleAction, error)
}

// credentialPooler is the part of the PgBouncer updater these steps use.
type credentialPooler interface {
	AddDatabase(ctx context.Context, dbName, roleName, rolePassword string) error
	EnsureDatabase(ctx context.Context, dbName string) (bool, error)
	HasUser(ctx context.Context, roleName string) (bool, error)
	EnsureUser(ctx context.Context, roleName, password string, replace bool) (bool, error)
	Restart(ctx context.Context) error
}

// credentialSecrets is the part of the Secrets provisioner these steps use.
type credentialSecrets interface {
	AppendEntries(ctx context.Context, namespace, project, secretName string, entries []types.SecretEntry) error
	Lookup(ctx context.Context, namespace, secretName, key string) ([]byte, bool, error)
}

// credentialDeps bundles the three provisioners; any may be nil (not configured).
type credentialDeps struct {
	pg      credentialPostgres
	pooler  credentialPooler
	secrets credentialSecrets
	logger  logging.Logger
	// generate draws a value; tests replace it to make values recognisable.
	generate func(nBytes int) (string, error)
}

// GeneratedCredential is one line of the response summary: names, never values.
type GeneratedCredential struct {
	Kind      string   `json:"kind"` // owner_role | app_role | secret
	Name      string   `json:"name"` // role name or Secret key
	Secret    string   `json:"secret"`
	Keys      []string `json:"keys,omitempty"`
	Action    string   `json:"action"`
	Database  string   `json:"database,omitempty"`
	ConnLimit int      `json:"connection_limit,omitempty"`
}

// credentialDeps converts the handler's concrete provisioners, keeping nil
// pointers nil (a typed nil inside an interface would read as configured).
func (h *Handler) credentialDeps() credentialDeps {
	d := credentialDeps{logger: h.logger, generate: provisioning.GenerateSecretValue}
	if h.postgresProvisioner != nil {
		d.pg = h.postgresProvisioner
	}
	if h.pgbouncerUpdater != nil {
		d.pooler = h.pgbouncerUpdater
	}
	if h.secretsProvisioner != nil { // pragma: allowlist secret -- provisioner/field names, not a value
		d.secrets = h.secretsProvisioner // pragma: allowlist secret -- provisioner/field names, not a value
	}
	return d
}

// provisionDatabaseCredentials runs the Postgres, app-role and generated-secret
// steps of onboarding, shared by OnboardRepo and EnsureOnboarding.
func (h *Handler) provisionDatabaseCredentials(ctx context.Context, steps *[]stepResult, req *types.OnboardingRequest, namespace string) []GeneratedCredential {
	results, stepList := runCredentialSteps(ctx, h.credentialDeps(), req, namespace)
	for _, s := range stepList {
		h.recordStep(ctx, steps, s.Name, false, s.Err)
	}
	return results
}

// credentialRun carries state across the steps of one request.
type credentialRun struct {
	deps          credentialDeps
	req           *types.OnboardingRequest
	namespace     string
	secretName    string
	poolerChanged bool
	results       []GeneratedCredential
	steps         []stepResult
}

func runCredentialSteps(ctx context.Context, deps credentialDeps, req *types.OnboardingRequest, namespace string) ([]GeneratedCredential, []stepResult) {
	if deps.generate == nil {
		deps.generate = provisioning.GenerateSecretValue
	}
	run := &credentialRun{
		deps:       deps,
		req:        req,
		namespace:  namespace,
		secretName: projectSecretName(req),
	}

	if req.ProvisionPostgres != nil {
		if req.ProvisionPostgres.GeneratePassword {
			run.step("postgres", run.generatedOwner(ctx))
		} else {
			run.step("postgres", run.typedOwner(ctx))
		}
	}
	if req.ProvisionAppRole != nil {
		run.step("app_role", run.appRole(ctx))
	}
	if len(req.GenerateSecrets) > 0 {
		run.step("generated_secrets", run.generatedSecrets(ctx))
	}
	if run.poolerChanged && deps.pooler != nil {
		run.step("pgbouncer_restart", deps.pooler.Restart(ctx))
	}
	return run.results, run.steps
}

func (r *credentialRun) step(name string, err error) {
	s := stepResult{Name: name, Status: "ok", Err: err}
	if err != nil {
		s.Status = "failed"
		s.Detail = err.Error()
	}
	r.steps = append(r.steps, s)
}

func projectSecretName(req *types.OnboardingRequest) string {
	if req.SecretName != "" {
		return req.SecretName
	}
	return req.ProjectName + "-credentials"
}

// typedOwner is the pre-existing caller-chosen password path, unchanged.
func (r *credentialRun) typedOwner(ctx context.Context) error {
	spec := r.req.ProvisionPostgres
	if r.deps.pg == nil {
		return fmt.Errorf("not configured (POSTGRES_ADMIN_URL not set)")
	}
	if err := r.deps.pg.Provision(ctx, spec); err != nil {
		return err
	}
	if r.deps.pooler != nil {
		if err := r.deps.pooler.AddDatabase(ctx, spec.DatabaseName, ownerRoleName(spec), spec.RolePassword); err != nil {
			r.step("pgbouncer", err)
		}
	}
	return nil
}

func ownerRoleName(spec *types.PostgresProvisionSpec) string {
	if spec.RoleName != "" {
		return spec.RoleName
	}
	return spec.DatabaseName
}

// decideRolePassword applies the keep / generate / refuse rule shared by the
// owner and app roles. It returns the new password ("" = keep) and the action.
func (r *credentialRun) decideRolePassword(ctx context.Context, role, urlKey string, rotate bool) (string, string, error) {
	exists, err := r.deps.pg.RoleExists(ctx, role)
	if err != nil {
		return "", "", err
	}
	_, stored, err := r.deps.secrets.Lookup(ctx, r.namespace, r.secretName, urlKey)
	if err != nil {
		return "", "", err
	}
	switch {
	case rotate:
		pw, err := r.deps.generate(provisioning.DefaultGeneratedSecretBytes)
		return pw, credentialRotated, err
	case !exists:
		pw, err := r.deps.generate(provisioning.DefaultGeneratedSecretBytes)
		return pw, credentialCreated, err
	case stored:
		return "", credentialKept, nil
	default:
		return "", "", fmt.Errorf("role %s exists but Secret %s/%s has no %s; refusing to rotate implicitly — "+
			"re-run with the rotate flag to generate a new password and write it", role, r.namespace, r.secretName, urlKey)
	}
}

// ensurePoolerUser makes the userlist carry the role. With a new password the
// line is rewritten; when the password was kept, a missing line is repaired
// from the URL already in the Secret.
func (r *credentialRun) ensurePoolerUser(ctx context.Context, role, dbName, urlKey, newPassword string) error {
	if r.deps.pooler == nil {
		return fmt.Errorf("pgbouncer updater not configured; %s cannot connect through the pooler", role)
	}
	changed, err := r.deps.pooler.EnsureDatabase(ctx, dbName)
	if err != nil {
		return err
	}
	r.poolerChanged = r.poolerChanged || changed

	password := newPassword // pragma: allowlist secret -- variable name; the value is generated at runtime
	if password == "" {
		has, err := r.deps.pooler.HasUser(ctx, role)
		if err != nil {
			return err
		}
		if has {
			return nil
		}
		raw, ok, err := r.deps.secrets.Lookup(ctx, r.namespace, r.secretName, urlKey)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("pooler has no line for %s and Secret key %s is absent", role, urlKey)
		}
		if password, err = provisioning.PasswordFromConnectionURL(string(raw), role); err != nil {
			return fmt.Errorf("repair pooler line for %s: %w", role, err)
		}
	}
	changed, err = r.deps.pooler.EnsureUser(ctx, role, password, newPassword != "")
	if err != nil {
		return err
	}
	r.poolerChanged = r.poolerChanged || changed
	return nil
}

func (r *credentialRun) generatedOwner(ctx context.Context) error {
	spec := r.req.ProvisionPostgres
	if r.deps.pg == nil {
		return fmt.Errorf("not configured (POSTGRES_ADMIN_URL not set)")
	}
	if r.deps.secrets == nil { // pragma: allowlist secret -- field name, not a value
		return fmt.Errorf("secrets provisioner not configured; a generated password would have nowhere to go")
	}
	role := ownerRoleName(spec)
	urlKey := spec.URLSecretKey
	if urlKey == "" {
		urlKey = defaultOwnerURLSecretKey
	}

	password, action, err := r.decideRolePassword(ctx, role, urlKey, spec.RotatePassword)
	if err != nil {
		return err
	}
	if err := r.deps.pg.ProvisionGeneratedOwner(ctx, spec, password); err != nil {
		return err
	}
	if password != "" {
		entry := types.SecretEntry{Key: urlKey, Value: provisioning.PooledConnectionURL(role, password, spec.DatabaseName)}
		if err := r.deps.secrets.AppendEntries(ctx, r.namespace, r.req.ProjectName, r.secretName, []types.SecretEntry{entry}); err != nil {
			return fmt.Errorf("owner password was set but %s could not be written (%w); re-run with the rotate flag", urlKey, err)
		}
	}
	if err := r.ensurePoolerUser(ctx, role, spec.DatabaseName, urlKey, password); err != nil {
		r.step("pgbouncer", err)
	}
	r.results = append(r.results, GeneratedCredential{
		Kind: "owner_role", Name: role, Secret: r.secretName, Keys: []string{urlKey}, // pragma: allowlist secret -- field names, not a value
		Action: action, Database: spec.DatabaseName, ConnLimit: spec.ConnectionLimit,
	})
	r.log(ctx, "owner_role", role, action)
	return nil
}

func (r *credentialRun) appRole(ctx context.Context) error {
	spec := resolvedAppRole(r.req)
	if r.deps.pg == nil {
		return fmt.Errorf("not configured (POSTGRES_ADMIN_URL not set)")
	}
	if r.deps.secrets == nil { // pragma: allowlist secret -- field name, not a value
		return fmt.Errorf("secrets provisioner not configured; a generated password would have nowhere to go")
	}
	if r.deps.pooler == nil {
		return fmt.Errorf("pgbouncer updater not configured; the app role could not connect through the pooler")
	}

	password, _, err := r.decideRolePassword(ctx, spec.RoleName, spec.URLSecretKey, spec.RotatePassword)
	if err != nil {
		return err
	}
	action, err := r.deps.pg.EnsureAppRole(ctx, provisioning.AppRole{
		RoleName: spec.RoleName, DatabaseName: spec.DatabaseName, ConnectionLimit: spec.ConnectionLimit,
	}, password)
	if err != nil {
		return err
	}
	keys := []string{spec.URLSecretKey}
	if password != "" {
		entries := []types.SecretEntry{{
			Key:   spec.URLSecretKey,
			Value: provisioning.PooledConnectionURL(spec.RoleName, password, spec.DatabaseName),
		}}
		if spec.PasswordSecretKey != "" {
			entries = append(entries, types.SecretEntry{Key: spec.PasswordSecretKey, Value: password})
		}
		if err := r.deps.secrets.AppendEntries(ctx, r.namespace, r.req.ProjectName, r.secretName, entries); err != nil {
			return fmt.Errorf("app role password was set but its Secret keys could not be written (%w); re-run with the rotate flag", err)
		}
	}
	if spec.PasswordSecretKey != "" {
		keys = append(keys, spec.PasswordSecretKey)
	}
	if err := r.ensurePoolerUser(ctx, spec.RoleName, spec.DatabaseName, spec.URLSecretKey, password); err != nil {
		return err
	}
	r.results = append(r.results, GeneratedCredential{
		Kind: "app_role", Name: spec.RoleName, Secret: r.secretName, Keys: keys, // pragma: allowlist secret -- field names, not a value
		Action: string(action), Database: spec.DatabaseName, ConnLimit: spec.ConnectionLimit,
	})
	r.log(ctx, "app_role", spec.RoleName, string(action))
	return nil
}

// resolvedAppRole applies the defaults to the request's app role.
func resolvedAppRole(req *types.OnboardingRequest) types.AppRoleSpec {
	spec := *req.ProvisionAppRole
	if spec.DatabaseName == "" && req.ProvisionPostgres != nil {
		spec.DatabaseName = req.ProvisionPostgres.DatabaseName
	}
	if spec.ConnectionLimit == 0 {
		spec.ConnectionLimit = provisioning.DefaultAppRoleConnectionLimit
	}
	if spec.URLSecretKey == "" {
		spec.URLSecretKey = defaultAppRoleURLSecretKey // pragma: allowlist secret -- provisioner/field names, not a value
	}
	return spec
}

func (r *credentialRun) generatedSecrets(ctx context.Context) error {
	if r.deps.secrets == nil { // pragma: allowlist secret -- field name, not a value
		return fmt.Errorf("secrets provisioner not configured (K8s client unavailable)")
	}
	var entries []types.SecretEntry
	var pending []GeneratedCredential
	for _, g := range r.req.GenerateSecrets {
		n := g.Bytes
		if n == 0 {
			n = provisioning.DefaultGeneratedSecretBytes
		}
		_, exists, err := r.deps.secrets.Lookup(ctx, r.namespace, r.secretName, g.Key)
		if err != nil {
			return err
		}
		item := GeneratedCredential{Kind: "secret", Name: g.Key, Secret: r.secretName, Keys: []string{g.Key}} // pragma: allowlist secret -- field names, not a value
		switch {
		case exists && !g.Rotate:
			item.Action = credentialKept
		default:
			value, err := r.deps.generate(n)
			if err != nil {
				return fmt.Errorf("generate %s: %w", g.Key, err)
			}
			entries = append(entries, types.SecretEntry{Key: g.Key, Value: value})
			item.Action = credentialCreated
			if exists {
				item.Action = credentialRotated
			}
		}
		pending = append(pending, item)
	}
	if len(entries) > 0 {
		if err := r.deps.secrets.AppendEntries(ctx, r.namespace, r.req.ProjectName, r.secretName, entries); err != nil {
			return err
		}
	}
	for _, item := range pending {
		r.log(ctx, "secret", item.Name, item.Action)
	}
	r.results = append(r.results, pending...)
	return nil
}

func (r *credentialRun) log(ctx context.Context, kind, name, action string) {
	if r.deps.logger == nil {
		return
	}
	r.deps.logger.Info(ctx, "Generated credential converged",
		logging.String("kind", kind),
		logging.String("name", name),
		logging.String("namespace", r.namespace),
		logging.String("secret", r.secretName),
		logging.String("action", action))
}
