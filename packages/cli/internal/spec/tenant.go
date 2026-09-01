package spec

// Tenant manifest — the CLIENT-IN-A-DAY schema.
//
// One document declares a whole client: the Janua organization that is its
// identity root, the Enclii runtime (project, namespace, apps, database,
// secrets, buckets, domains), the Nauta workspace, and the Kalya tenant.
//
// See docs/rfcs/2026-09-01-client-in-a-day.md for the orchestration order, the
// idempotency contract, and the sibling-platform gaps that keep execution
// unimplemented today. This file carries the schema and its validation; nothing
// here talks to a cluster or to a sibling platform.
//
// Deliberate non-goals encoded in the shape:
//
//   - An app's build/runtime/probe/SLO configuration is NOT restated here. Apps
//     reference their own enclii.yaml via `manifest`. Two sources of truth for a
//     port number is a bug generator.
//   - Secret VALUES never appear. `secrets[].keys` declares the contract so a
//     missing key is a loud plan failure; values move through
//     `enclii admin provision secrets --secrets-file`.
//   - The Kalya tenant file is REFERENCED, not inlined. Its schema is Kalya's.

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// TenantAPIVersion and TenantKind identify a tenant manifest document.
const (
	TenantAPIVersion = "enclii.dev/v1alpha"
	TenantKind       = "Tenant"
)

// maxTenantSlugLen is the cross-platform floor for a client slug.
//
// Nauta's workspaces.slug is VarChar(63); Kalya allows 120 and Janua 100. The
// binding constraint is therefore 63, and it is enforced here rather than
// discovered at step 11 of an apply that has already created an organization.
const maxTenantSlugLen = 63

// TenantSpecDoc is a `kind: Tenant` manifest.
type TenantSpecDoc struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind" json:"kind"`
	Metadata   TenantMetadata `yaml:"metadata" json:"metadata"`
	Spec       TenantSpec     `yaml:"spec" json:"spec"`
}

// TenantMetadata identifies the client.
type TenantMetadata struct {
	// Name is THE joining key. The Janua org slug, the Nauta workspace slug,
	// the Kalya tenant slug and the Enclii team slug are all this value.
	// Letting them diverge is how a workspace ends up unable to find its org.
	Name        string `yaml:"name" json:"name"`
	DisplayName string `yaml:"displayName,omitempty" json:"displayName,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// TenantSpec is the body of a tenant manifest.
type TenantSpec struct {
	Janua     TenantJanua     `yaml:"janua" json:"janua"`
	Project   string          `yaml:"project,omitempty" json:"project,omitempty"`
	Namespace string          `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Apps      []TenantApp     `yaml:"apps" json:"apps"`
	DB        *TenantDB       `yaml:"db,omitempty" json:"db,omitempty"`
	Secrets   []TenantSecret  `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Buckets   []TenantBucket  `yaml:"buckets,omitempty" json:"buckets,omitempty"`
	Nauta     *TenantNauta    `yaml:"nauta,omitempty" json:"nauta,omitempty"`
	Kalya     *TenantKalyaRef `yaml:"kalya,omitempty" json:"kalya,omitempty"`
}

// TenantJanua declares the identity root: the organization, its product
// entitlements, and the OAuth clients the apps authenticate against.
type TenantJanua struct {
	Org          TenantJanuaOrg      `yaml:"org" json:"org"`
	Tiers        map[string]string   `yaml:"tiers,omitempty" json:"tiers,omitempty"`
	OAuthClients []TenantOAuthClient `yaml:"oauthClients,omitempty" json:"oauthClients,omitempty"`
}

// TenantJanuaOrg is the organization to create or adopt.
type TenantJanuaOrg struct {
	// Slug and Name default to metadata.name / metadata.displayName.
	Slug string `yaml:"slug,omitempty" json:"slug,omitempty"`
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// OwnerEmail must already resolve to a Janua user. Janua resolves the
	// owner before any write so a bad owner cannot orphan a half-created org.
	OwnerEmail   string `yaml:"ownerEmail" json:"ownerEmail"`
	BillingEmail string `yaml:"billingEmail,omitempty" json:"billingEmail,omitempty"`
}

// TenantOAuthClient is one Janua OAuth client.
//
// A dedicated non-production client is the norm, not an exception: adding a
// staging redirect URI to the production client is how a staging login bounces
// into production.
type TenantOAuthClient struct {
	LogicalKey   string   `yaml:"logicalKey" json:"logicalKey"`
	Audience     string   `yaml:"audience,omitempty" json:"audience,omitempty"`
	RedirectURIs []string `yaml:"redirectURIs" json:"redirectURIs"`
}

// TenantApp is one application, across all of its environments.
type TenantApp struct {
	Name string `yaml:"name" json:"name"`
	Repo string `yaml:"repo" json:"repo"`
	// Manifest references the app's own enclii.yaml, which stays the authority
	// for build, runtime, probes, resources, SLO and network policy.
	Manifest     string            `yaml:"manifest,omitempty" json:"manifest,omitempty"`
	Environments []TenantAppEnv    `yaml:"environments" json:"environments"`
	Env          map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// TenantAppEnv is one environment of one app.
type TenantAppEnv struct {
	Name       string            `yaml:"name" json:"name"`
	AutoDeploy *bool             `yaml:"autoDeploy,omitempty" json:"autoDeploy,omitempty"`
	Domains    []TenantDomain    `yaml:"domains,omitempty" json:"domains,omitempty"`
	EnvFrom    []TenantEnvFrom   `yaml:"envFrom,omitempty" json:"envFrom,omitempty"`
	Env        map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// TenantDomain is one public hostname.
type TenantDomain struct {
	Host string `yaml:"host" json:"host"`
	TLS  *bool  `yaml:"tls,omitempty" json:"tls,omitempty"`
}

// TenantEnvFrom sources an environment's variables from a declared secret.
type TenantEnvFrom struct {
	Secret string `yaml:"secret" json:"secret"`
}

// TenantDB declares the managed Postgres database and any clones.
type TenantDB struct {
	Name       string   `yaml:"name" json:"name"`
	Extensions []string `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	// RLS is advisory. Enclii provisions the database and the role; whether row
	// level security is enabled inside it belongs to the app's migrations.
	RLS    *bool           `yaml:"rls,omitempty" json:"rls,omitempty"`
	Clones []TenantDBClone `yaml:"clones,omitempty" json:"clones,omitempty"`
}

// TenantDBClone is a database cloned from another on the SAME instance under
// the SAME owner.
//
// Same-owner is the point, not an accident: a new addon means a new DB role,
// which means appending to the static pgbouncer userlist, and a botched
// hand-edit of that userlist drops users so pooled auth fails cluster-wide
// while direct still works (2026-08-24).
type TenantDBClone struct {
	Name string `yaml:"name" json:"name"`
	From string `yaml:"from" json:"from"`
}

// TenantSecret declares a secret's KEY CONTRACT. Values never appear here.
type TenantSecret struct {
	Name string   `yaml:"name" json:"name"`
	Keys []string `yaml:"keys" json:"keys"`
}

// TenantBucket is an object-storage bucket.
type TenantBucket struct {
	Name     string `yaml:"name" json:"name"`
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
}

// TenantNauta declares the vCTO engagement workspace.
type TenantNauta struct {
	Workspace TenantNautaWorkspace `yaml:"workspace" json:"workspace"`
}

// TenantNautaWorkspace mirrors the fields nauta's createWorkspace accepts.
type TenantNautaWorkspace struct {
	Tier      string                `yaml:"tier,omitempty" json:"tier,omitempty"`
	Locale    string                `yaml:"locale,omitempty" json:"locale,omitempty"`
	Currency  string                `yaml:"currency,omitempty" json:"currency,omitempty"`
	Timezone  string                `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	Hostnames []TenantNautaHostname `yaml:"hostnames,omitempty" json:"hostnames,omitempty"`
}

// TenantNautaHostname is one workspace hostname. Exactly one must be primary.
type TenantNautaHostname struct {
	Host    string `yaml:"host" json:"host"`
	Primary bool   `yaml:"primary,omitempty" json:"primary,omitempty"`
}

// TenantKalyaRef references Kalya's own tenant configuration file.
//
// The tenant id is deliberately absent: Kalya's Tenant.id IS the Janua org UUID
// and is immutable after creation, so `tenant apply` resolves it from step 1
// and passes it through. Declaring it here would create a second source of
// truth for a value that throws on mismatch.
type TenantKalyaRef struct {
	TenantFile string `yaml:"tenantFile" json:"tenantFile"`
}

// validNautaTiers is nauta's ClientTier enum.
var validNautaTiers = map[string]bool{
	"SELF_SERVE":     true,
	"PROJECT":        true,
	"FRACTIONAL_CTO": true,
}

// validJanuaTiers is janua's product-tier vocabulary for organizations.product_tiers.
//
// An absent product means community/self-hosted, which is not the same as
// unentitled-and-broken — so an empty tiers map is valid.
var validJanuaTiers = map[string]bool{
	"essentials": true,
	"pro":        true,
	"madfam":     true,
}

// ParseTenantSpec reads and validates a tenant manifest.
//
// Unlike ParseServiceSpecNamed this rejects multi-document files outright: a
// tenant manifest describes exactly one client, and silently using the first of
// several documents is how an operator provisions the wrong one.
func ParseTenantSpec(path string) (*TenantSpecDoc, error) {
	// #nosec G304 -- the path is the operator's own `-f` argument, read on their
	// behalf with their own privileges. Identical trust model to every other
	// file flag in this CLI (parser.go, onboard.go's --secrets-file). There is
	// no boundary being crossed: a caller who can pass -f can already read the
	// file.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read tenant manifest: %w", err)
	}
	return ParseTenantSpecBytes(data)
}

// ParseTenantSpecBytes parses and validates a tenant manifest from memory.
func ParseTenantSpecBytes(data []byte) (*TenantSpecDoc, error) {
	var doc TenantSpecDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse tenant manifest YAML: %w", err)
	}
	if err := ValidateTenantSpec(&doc); err != nil {
		return nil, err
	}
	ApplyTenantDefaults(&doc)
	return &doc, nil
}

// ApplyTenantDefaults fills the fields that derive from metadata.name.
//
// Called after validation so a defaulted value can never mask a missing
// required one.
func ApplyTenantDefaults(doc *TenantSpecDoc) {
	if doc.Spec.Janua.Org.Slug == "" {
		doc.Spec.Janua.Org.Slug = doc.Metadata.Name
	}
	if doc.Spec.Janua.Org.Name == "" {
		if doc.Metadata.DisplayName != "" {
			doc.Spec.Janua.Org.Name = doc.Metadata.DisplayName
		} else {
			doc.Spec.Janua.Org.Name = doc.Metadata.Name
		}
	}
	if doc.Spec.Project == "" {
		doc.Spec.Project = doc.Metadata.Name
	}
	if doc.Spec.Namespace == "" {
		doc.Spec.Namespace = doc.Spec.Project
	}
	if doc.Spec.Nauta != nil && doc.Spec.Nauta.Workspace.Tier == "" {
		doc.Spec.Nauta.Workspace.Tier = "FRACTIONAL_CTO"
	}
	for i := range doc.Spec.Buckets {
		if doc.Spec.Buckets[i].Provider == "" {
			doc.Spec.Buckets[i].Provider = "r2"
		}
	}
}

// ValidateTenantSpec checks every constraint that has already caused an outage,
// plus the cross-references that make a plan coherent.
//
// It reports EVERY problem it finds rather than the first. An operator fixing a
// tenant manifest one error per run is an operator who runs it eight times.
func ValidateTenantSpec(doc *TenantSpecDoc) error {
	var errs []ValidationError

	if doc.APIVersion == "" {
		errs = append(errs, ValidationError{"apiVersion", "is required"})
	} else if doc.APIVersion != TenantAPIVersion {
		errs = append(errs, ValidationError{"apiVersion", fmt.Sprintf("must be %q", TenantAPIVersion)})
	}
	if doc.Kind == "" {
		errs = append(errs, ValidationError{"kind", "is required"})
	} else if doc.Kind != TenantKind {
		errs = append(errs, ValidationError{"kind", fmt.Sprintf("must be %q", TenantKind)})
	}

	errs = append(errs, validateTenantMetadata(&doc.Metadata)...)
	errs = append(errs, validateTenantJanua(&doc.Spec.Janua)...)
	errs = append(errs, validateTenantRuntime(&doc.Spec)...)
	errs = append(errs, validateTenantDB(doc.Spec.DB)...)
	errs = append(errs, validateTenantSecrets(doc.Spec.Secrets)...)
	errs = append(errs, validateTenantNauta(doc.Spec.Nauta)...)
	errs = append(errs, validateTenantKalya(doc.Spec.Kalya)...)
	errs = append(errs, validateTenantCrossRefs(&doc.Spec)...)

	if len(errs) > 0 {
		return tenantValidationError(errs)
	}
	return nil
}

func validateTenantMetadata(md *TenantMetadata) []ValidationError {
	var errs []ValidationError
	if md.Name == "" {
		errs = append(errs, ValidationError{"metadata.name", "is required"})
		return errs
	}
	if !isValidDNSName(md.Name) {
		errs = append(errs, ValidationError{
			"metadata.name",
			"must be lowercase letters, numbers and hyphens, not starting or ending with a hyphen",
		})
	}
	if len(md.Name) > maxTenantSlugLen {
		errs = append(errs, ValidationError{
			"metadata.name",
			fmt.Sprintf("must be at most %d characters (nauta workspaces.slug is VarChar(%d) — the cross-platform floor)", maxTenantSlugLen, maxTenantSlugLen),
		})
	}
	return errs
}

func validateTenantJanua(j *TenantJanua) []ValidationError {
	var errs []ValidationError

	if j.Org.OwnerEmail == "" {
		errs = append(errs, ValidationError{
			"spec.janua.org.ownerEmail",
			"is required — janua resolves the owner before any write, and an org created without one is owned by the operator rather than the client",
		})
	} else if !strings.Contains(j.Org.OwnerEmail, "@") {
		errs = append(errs, ValidationError{"spec.janua.org.ownerEmail", "must be an email address"})
	}
	if j.Org.Slug != "" {
		if !isValidDNSName(j.Org.Slug) {
			errs = append(errs, ValidationError{"spec.janua.org.slug", "must match ^[a-z0-9-]+$ (janua's slug pattern)"})
		}
		if len(j.Org.Slug) > maxTenantSlugLen {
			errs = append(errs, ValidationError{"spec.janua.org.slug", fmt.Sprintf("must be at most %d characters", maxTenantSlugLen)})
		}
	}

	for _, product := range sortedKeys(j.Tiers) {
		tier := j.Tiers[product]
		if !validJanuaTiers[tier] {
			errs = append(errs, ValidationError{
				fmt.Sprintf("spec.janua.tiers.%s", product),
				fmt.Sprintf("must be one of: essentials, pro, madfam (got %q)", tier),
			})
		}
	}

	seenKeys := map[string]bool{}
	for i, c := range j.OAuthClients {
		field := fmt.Sprintf("spec.janua.oauthClients[%d]", i)
		if c.LogicalKey == "" {
			errs = append(errs, ValidationError{field + ".logicalKey", "is required"})
		} else if seenKeys[c.LogicalKey] {
			errs = append(errs, ValidationError{field + ".logicalKey", fmt.Sprintf("duplicate logical key %q", c.LogicalKey)})
		} else {
			seenKeys[c.LogicalKey] = true
		}
		if len(c.RedirectURIs) == 0 {
			errs = append(errs, ValidationError{
				field + ".redirectURIs",
				"at least one redirect URI is required — a client with none cannot complete a login",
			})
		}
		for k, uri := range c.RedirectURIs {
			if !strings.HasPrefix(uri, "https://") {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.redirectURIs[%d]", field, k),
					"must be an absolute https:// URL",
				})
			}
		}
	}
	return errs
}

func validateTenantRuntime(s *TenantSpec) []ValidationError {
	var errs []ValidationError

	if s.Project != "" && !isValidDNSName(s.Project) {
		errs = append(errs, ValidationError{"spec.project", "must be a valid DNS name"})
	}
	if s.Namespace != "" && !isValidDNSName(s.Namespace) {
		errs = append(errs, ValidationError{"spec.namespace", "must be a valid DNS name"})
	}
	if len(s.Apps) == 0 {
		errs = append(errs, ValidationError{"spec.apps", "at least one app is required"})
	}

	seenApps := map[string]bool{}
	for i, app := range s.Apps {
		field := fmt.Sprintf("spec.apps[%d]", i)
		switch {
		case app.Name == "":
			errs = append(errs, ValidationError{field + ".name", "is required"})
		case !isValidDNSName(app.Name):
			errs = append(errs, ValidationError{field + ".name", "must be a valid DNS name"})
		case seenApps[app.Name]:
			errs = append(errs, ValidationError{field + ".name", fmt.Sprintf("duplicate app %q", app.Name)})
		default:
			seenApps[app.Name] = true
		}

		if app.Repo == "" {
			errs = append(errs, ValidationError{field + ".repo", "is required (org/name)"})
		} else if strings.Count(app.Repo, "/") != 1 {
			errs = append(errs, ValidationError{field + ".repo", "must be in org/name format"})
		}

		if len(app.Environments) == 0 {
			errs = append(errs, ValidationError{field + ".environments", "at least one environment is required"})
		}
		seenEnvs := map[string]bool{}
		for k, env := range app.Environments {
			envField := fmt.Sprintf("%s.environments[%d]", field, k)
			switch {
			case env.Name == "":
				errs = append(errs, ValidationError{envField + ".name", "is required"})
			case seenEnvs[env.Name]:
				errs = append(errs, ValidationError{envField + ".name", fmt.Sprintf("duplicate environment %q for app %q", env.Name, app.Name)})
			default:
				seenEnvs[env.Name] = true
			}
			errs = append(errs, validateTenantEnvVarNames(envField, env.Env)...)
		}
		errs = append(errs, validateTenantEnvVarNames(field, app.Env)...)
	}
	return errs
}

func validateTenantEnvVarNames(field string, env map[string]string) []ValidationError {
	var errs []ValidationError
	for _, name := range sortedKeys(env) {
		if !isValidEnvVarName(name) {
			errs = append(errs, ValidationError{
				fmt.Sprintf("%s.env.%s", field, name),
				"environment variable names must be letters, numbers and underscores, not starting with a number",
			})
		}
	}
	return errs
}

func validateTenantDB(db *TenantDB) []ValidationError {
	if db == nil {
		return nil
	}
	var errs []ValidationError
	if db.Name == "" {
		errs = append(errs, ValidationError{"spec.db.name", "is required"})
	}
	seen := map[string]bool{db.Name: true}
	for i, clone := range db.Clones {
		field := fmt.Sprintf("spec.db.clones[%d]", i)
		if clone.Name == "" {
			errs = append(errs, ValidationError{field + ".name", "is required"})
		} else if seen[clone.Name] {
			errs = append(errs, ValidationError{field + ".name", fmt.Sprintf("duplicate database name %q", clone.Name)})
		} else {
			seen[clone.Name] = true
		}
		if clone.From == "" {
			errs = append(errs, ValidationError{field + ".from", "is required"})
		} else if clone.From != db.Name {
			// Cloning from a database this manifest does not declare means the
			// clone lands on an instance and owner we cannot reason about — and
			// a different owner is a new DB role, which is a pgbouncer userlist
			// hand-edit, which is the 2026-08-24 outage class.
			errs = append(errs, ValidationError{
				field + ".from",
				fmt.Sprintf("must reference this manifest's database %q — cloning across owners requires a new DB role and a pgbouncer userlist edit", db.Name),
			})
		}
	}
	return errs
}

func validateTenantSecrets(secrets []TenantSecret) []ValidationError {
	var errs []ValidationError
	seen := map[string]bool{}
	for i, s := range secrets {
		field := fmt.Sprintf("spec.secrets[%d]", i)
		switch {
		case s.Name == "":
			errs = append(errs, ValidationError{field + ".name", "is required"})
		case seen[s.Name]:
			errs = append(errs, ValidationError{field + ".name", fmt.Sprintf("duplicate secret %q", s.Name)})
		default:
			seen[s.Name] = true
		}
		if len(s.Keys) == 0 {
			errs = append(errs, ValidationError{field + ".keys", "at least one key is required"})
		}
		seenKeys := map[string]bool{}
		for k, key := range s.Keys {
			keyField := fmt.Sprintf("%s.keys[%d]", field, k)
			// A manifest is committed; a value in it is a leaked value. Catch
			// the KEY=VALUE shape rather than trusting review to notice.
			if strings.Contains(key, "=") {
				errs = append(errs, ValidationError{
					keyField,
					"must be a key NAME only — secret values never appear in a manifest (provision them with `enclii admin provision secrets --secrets-file`)",
				})
				continue
			}
			if !isValidEnvVarName(key) {
				errs = append(errs, ValidationError{keyField, "must be a valid environment variable name"})
			}
			if seenKeys[key] {
				errs = append(errs, ValidationError{keyField, fmt.Sprintf("duplicate key %q", key)})
			}
			seenKeys[key] = true
		}
	}
	return errs
}

func validateTenantNauta(n *TenantNauta) []ValidationError {
	if n == nil {
		return nil
	}
	var errs []ValidationError
	w := n.Workspace
	if w.Tier != "" && !validNautaTiers[w.Tier] {
		errs = append(errs, ValidationError{
			"spec.nauta.workspace.tier",
			fmt.Sprintf("must be one of: SELF_SERVE, PROJECT, FRACTIONAL_CTO (got %q)", w.Tier),
		})
	}
	if w.Currency != "" && len(w.Currency) != 3 {
		errs = append(errs, ValidationError{"spec.nauta.workspace.currency", "must be a 3-letter ISO 4217 code"})
	}

	primaries := 0
	seen := map[string]bool{}
	for i, h := range w.Hostnames {
		field := fmt.Sprintf("spec.nauta.workspace.hostnames[%d]", i)
		if h.Host == "" {
			errs = append(errs, ValidationError{field + ".host", "is required"})
			continue
		}
		if seen[h.Host] {
			errs = append(errs, ValidationError{field + ".host", fmt.Sprintf("duplicate hostname %q", h.Host)})
		}
		seen[h.Host] = true
		if h.Primary {
			primaries++
		}
	}
	// Nauta documents "exactly one primary per workspace" but enforces it by
	// convention, not by a DB constraint. Catching it here is cheaper than
	// discovering it as ambiguous routing later.
	if len(w.Hostnames) > 0 && primaries != 1 {
		errs = append(errs, ValidationError{
			"spec.nauta.workspace.hostnames",
			fmt.Sprintf("exactly one hostname must be primary (found %d) — nauta expects this but does not enforce it in the database", primaries),
		})
	}
	return errs
}

func validateTenantKalya(k *TenantKalyaRef) []ValidationError {
	if k == nil {
		return nil
	}
	if k.TenantFile == "" {
		return []ValidationError{{
			"spec.kalya.tenantFile",
			"is required when a kalya block is present — the tenant configuration is referenced, never inlined",
		}}
	}
	return nil
}

// validateTenantCrossRefs checks the constraints that span blocks: every
// envFrom names a declared secret, and every domain host is unique across the
// whole manifest and safe to put behind Universal SSL.
func validateTenantCrossRefs(s *TenantSpec) []ValidationError {
	var errs []ValidationError

	declaredSecrets := map[string]bool{}
	for _, sec := range s.Secrets {
		declaredSecrets[sec.Name] = true
	}

	// host -> "app/env" that first claimed it.
	claimed := map[string]string{}

	for _, app := range s.Apps {
		for _, env := range app.Environments {
			where := fmt.Sprintf("%s/%s", app.Name, env.Name)

			for _, from := range env.EnvFrom {
				if from.Secret == "" {
					errs = append(errs, ValidationError{
						fmt.Sprintf("spec.apps[%s].environments[%s].envFrom", app.Name, env.Name),
						"secret name is required",
					})
					continue
				}
				if !declaredSecrets[from.Secret] {
					errs = append(errs, ValidationError{
						fmt.Sprintf("spec.apps[%s].environments[%s].envFrom", app.Name, env.Name),
						fmt.Sprintf("references secret %q which this manifest does not declare — declare its key contract under spec.secrets", from.Secret),
					})
				}
			}

			for _, d := range env.Domains {
				field := fmt.Sprintf("spec.apps[%s].environments[%s].domains", app.Name, env.Name)
				if d.Host == "" {
					errs = append(errs, ValidationError{field, "host is required"})
					continue
				}
				if owner, dup := claimed[d.Host]; dup {
					// Two services answering one host is not a preference
					// question: the second capture rewrites the first's tunnel
					// route to a different backend.
					errs = append(errs, ValidationError{
						field,
						fmt.Sprintf("host %q is already declared by %s — one host resolves to exactly one backend", d.Host, owner),
					})
					continue
				}
				claimed[d.Host] = where
				if err := validateFlatLabelHost(field, d.Host); err != nil {
					errs = append(errs, *err)
				}
			}
		}
	}
	return errs
}

// validateFlatLabelHost enforces the flat-label domain rule.
//
// Cloudflare Universal SSL covers an apex and ONE label below it. `a.b.c.mx` is
// two levels below the apex and gets no certificate — so the host resolves,
// serves a TLS error, and looks like an outage. crea-map's manifest carries this
// rule as a comment; this makes it a check.
func validateFlatLabelHost(field, host string) *ValidationError {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return &ValidationError{field, fmt.Sprintf("host %q must be a bare hostname, not a URL", host)}
	}
	if strings.ContainsAny(host, "/:") {
		return &ValidationError{field, fmt.Sprintf("host %q must not contain a path or port", host)}
	}
	if host != strings.ToLower(host) {
		return &ValidationError{field, fmt.Sprintf("host %q must be lowercase", host)}
	}
	labels := strings.Split(host, ".")
	for _, l := range labels {
		if l == "" {
			return &ValidationError{field, fmt.Sprintf("host %q has an empty label", host)}
		}
	}
	if len(labels) < 3 {
		return &ValidationError{field, fmt.Sprintf("host %q must be a subdomain of an apex (e.g. app.example.mx)", host)}
	}
	if len(labels) > 3 {
		return &ValidationError{
			field,
			fmt.Sprintf("host %q nests %d labels below the apex — Cloudflare Universal SSL covers only one, so a nested host serves a TLS error that reads as an outage; use a flat label such as %s.%s",
				host, len(labels)-2, strings.Join(labels[:len(labels)-2], "-"), strings.Join(labels[len(labels)-2:], ".")),
		}
	}
	return nil
}

// tenantValidationError renders every finding, one per line, so a single run
// tells an operator everything that is wrong.
func tenantValidationError(errs []ValidationError) error {
	var b strings.Builder
	fmt.Fprintf(&b, "tenant manifest validation failed (%d problem(s)):", len(errs))
	for _, e := range errs {
		fmt.Fprintf(&b, "\n  - %s: %s", e.Field, e.Message)
	}
	return fmt.Errorf("%s", b.String())
}

// SortedTierProducts returns a tiers map's product names in a stable order, so
// plan output does not reshuffle between runs of an unchanged manifest.
func SortedTierProducts(tiers map[string]string) []string {
	return sortedKeys(tiers)
}

// sortedKeys keeps validation output deterministic. Map iteration order would
// otherwise reorder findings between runs and make diffs unreadable.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
