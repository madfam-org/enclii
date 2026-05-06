package types

// EcosystemApp is the MADFAM AppSpec contract shared by Janua, Enclii, and Selva.
// It mirrors apiVersion=madfam.io/v1alpha1, kind=EcosystemApp.
type EcosystemApp struct {
	APIVersion string               `json:"apiVersion" yaml:"apiVersion"`
	Kind       string               `json:"kind" yaml:"kind"`
	Metadata   EcosystemAppMetadata `json:"metadata" yaml:"metadata"`
	Spec       EcosystemAppSpec     `json:"spec" yaml:"spec"`
}

// EcosystemAppMetadata identifies the app/environment and desired-state hash.
type EcosystemAppMetadata struct {
	AppID            string            `json:"app_id" yaml:"app_id"`
	OwnerOrgID       string            `json:"owner_org_id" yaml:"owner_org_id"`
	Environment      string            `json:"environment" yaml:"environment"`
	IdempotencyKey   string            `json:"idempotency_key" yaml:"idempotency_key"`
	DesiredStateHash string            `json:"desired_state_hash" yaml:"desired_state_hash"`
	Labels           map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations      map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// EcosystemAppSpec groups identity, runtime, deployment, orchestration, and evidence posture.
type EcosystemAppSpec struct {
	Identity      EcosystemIdentity      `json:"identity" yaml:"identity"`
	Runtime       EcosystemRuntime       `json:"runtime" yaml:"runtime"`
	Deployment    EcosystemDeployment    `json:"deployment" yaml:"deployment"`
	Orchestration EcosystemOrchestration `json:"orchestration" yaml:"orchestration"`
	Observability EcosystemObservability `json:"observability" yaml:"observability"`
}

// EcosystemIdentity describes Janua OAuth clients, audiences, scopes, roles, and org bindings.
type EcosystemIdentity struct {
	Issuer       string                 `json:"issuer" yaml:"issuer"`
	JWKSURI      string                 `json:"jwks_uri,omitempty" yaml:"jwks_uri,omitempty"`
	OAuthClients []EcosystemOAuthClient `json:"oauth_clients" yaml:"oauth_clients"`
	Audiences    []EcosystemAudience    `json:"audiences" yaml:"audiences"`
	Scopes       []EcosystemScope       `json:"scopes" yaml:"scopes"`
	Roles        []EcosystemRole        `json:"roles" yaml:"roles"`
	OrgBindings  []EcosystemOrgBinding  `json:"org_bindings" yaml:"org_bindings"`
}

// EcosystemOAuthClient describes one Janua OAuth client.
type EcosystemOAuthClient struct {
	LogicalKey             string   `json:"logical_key" yaml:"logical_key"`
	ClientID               string   `json:"client_id" yaml:"client_id"`
	DisplayName            string   `json:"display_name" yaml:"display_name"`
	ClientType             string   `json:"client_type" yaml:"client_type"`
	IsConfidential         bool     `json:"is_confidential" yaml:"is_confidential"`
	Audience               string   `json:"audience" yaml:"audience"`
	RedirectURIs           []string `json:"redirect_uris" yaml:"redirect_uris"`
	PostLogoutRedirectURIs []string `json:"post_logout_redirect_uris" yaml:"post_logout_redirect_uris"`
	AllowedOrigins         []string `json:"allowed_origins" yaml:"allowed_origins"`
	GrantTypes             []string `json:"grant_types" yaml:"grant_types"`
	ResponseTypes          []string `json:"response_types" yaml:"response_types"`
	Scopes                 []string `json:"scopes" yaml:"scopes"`
}

type EcosystemAudience struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
}

type EcosystemScope struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
}

type EcosystemRole struct {
	Name   string   `json:"name" yaml:"name"`
	Scopes []string `json:"scopes" yaml:"scopes"`
}

type EcosystemOrgBinding struct {
	OrgID string   `json:"org_id" yaml:"org_id"`
	Roles []string `json:"roles" yaml:"roles"`
	Tiers []string `json:"tiers" yaml:"tiers"`
}

// EcosystemRuntime describes Enclii-owned runtime primitives.
type EcosystemRuntime struct {
	Namespace       string                   `json:"namespace" yaml:"namespace"`
	Services        []EcosystemService       `json:"services" yaml:"services"`
	Databases       []EcosystemDatabase      `json:"databases" yaml:"databases"`
	Buckets         []EcosystemBucket        `json:"buckets" yaml:"buckets"`
	Secrets         []EcosystemSecret        `json:"secrets" yaml:"secrets"`
	Domains         []EcosystemDomain        `json:"domains" yaml:"domains"`
	NetworkPolicies []EcosystemNetworkPolicy `json:"network_policies" yaml:"network_policies"`
}

type EcosystemService struct {
	Name         string `json:"name" yaml:"name"`
	Kind         string `json:"kind" yaml:"kind"`
	Port         int    `json:"port" yaml:"port"`
	Public       bool   `json:"public" yaml:"public"`
	HealthPath   string `json:"health_path" yaml:"health_path"`
	RequiresAuth bool   `json:"requires_auth,omitempty" yaml:"requires_auth,omitempty"`
}

type EcosystemDatabase struct {
	Name            string `json:"name" yaml:"name"`
	Engine          string `json:"engine" yaml:"engine"`
	LogicalDatabase string `json:"logical_database" yaml:"logical_database"`
	OwnerService    string `json:"owner_service" yaml:"owner_service"`
	HARequired      bool   `json:"ha_required,omitempty" yaml:"ha_required,omitempty"`
}

type EcosystemBucket struct {
	Name     string `json:"name" yaml:"name"`
	Provider string `json:"provider" yaml:"provider"`
	Purpose  string `json:"purpose" yaml:"purpose"`
}

type EcosystemSecret struct {
	Name         string   `json:"name" yaml:"name"`
	Keys         []string `json:"keys" yaml:"keys"`
	Source       string   `json:"source" yaml:"source"`
	RotationDays int      `json:"rotation_days" yaml:"rotation_days"`
}

type EcosystemDomain struct {
	Host             string `json:"host" yaml:"host"`
	Service          string `json:"service" yaml:"service"`
	TLS              bool   `json:"tls" yaml:"tls"`
	CloudflareTunnel string `json:"cloudflare_tunnel" yaml:"cloudflare_tunnel"`
}

type EcosystemNetworkPolicy struct {
	Service string   `json:"service" yaml:"service"`
	Ingress []string `json:"ingress" yaml:"ingress"`
	Egress  []string `json:"egress" yaml:"egress"`
}

// EcosystemDeployment describes GitOps deployment facts and rollback posture.
type EcosystemDeployment struct {
	Repo            string                  `json:"repo" yaml:"repo"`
	Branch          string                  `json:"branch" yaml:"branch"`
	ManifestPath    string                  `json:"manifest_path" yaml:"manifest_path"`
	GitOpsApp       string                  `json:"gitops_app" yaml:"gitops_app"`
	CurrentPointer  string                  `json:"current_pointer" yaml:"current_pointer"`
	RollbackPointer string                  `json:"rollback_pointer" yaml:"rollback_pointer"`
	Images          []EcosystemImage        `json:"images" yaml:"images"`
	HealthChecks    []EcosystemCheck        `json:"health_checks" yaml:"health_checks"`
	SmokeChecks     []EcosystemCheck        `json:"smoke_checks" yaml:"smoke_checks"`
	RollbackPolicy  EcosystemRollbackPolicy `json:"rollback_policy" yaml:"rollback_policy"`
}

type EcosystemImage struct {
	Service    string `json:"service" yaml:"service"`
	Repository string `json:"repository" yaml:"repository"`
	Digest     string `json:"digest" yaml:"digest"`
}

type EcosystemCheck struct {
	Name           string `json:"name" yaml:"name"`
	Type           string `json:"type" yaml:"type"`
	Target         string `json:"target" yaml:"target"`
	ExpectedStatus int    `json:"expected_status,omitempty" yaml:"expected_status,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds" yaml:"timeout_seconds"`
}

type EcosystemRollbackPolicy struct {
	Strategy         string `json:"strategy" yaml:"strategy"`
	RequiresApproval bool   `json:"requires_approval" yaml:"requires_approval"`
	RollbackPointer  string `json:"rollback_pointer,omitempty" yaml:"rollback_pointer,omitempty"`
}

// EcosystemOrchestration describes Selva policy for AppSpec-driven work.
type EcosystemOrchestration struct {
	Audience         string   `json:"audience" yaml:"audience"`
	ApprovalPolicy   string   `json:"approval_policy" yaml:"approval_policy"`
	AllowedModes     []string `json:"allowed_modes" yaml:"allowed_modes"`
	MaxRetryAttempts int      `json:"max_retry_attempts" yaml:"max_retry_attempts"`
	TimeoutSeconds   int      `json:"timeout_seconds" yaml:"timeout_seconds"`
	SoakSeconds      int      `json:"soak_seconds" yaml:"soak_seconds"`
	PolicyTags       []string `json:"policy_tags,omitempty" yaml:"policy_tags,omitempty"`
}

// EcosystemObservability describes health, alerting, dashboards, and evidence retention.
type EcosystemObservability struct {
	SLOs                  []EcosystemSLO       `json:"slos" yaml:"slos"`
	Alerts                []EcosystemAlert     `json:"alerts" yaml:"alerts"`
	Dashboards            []EcosystemDashboard `json:"dashboards" yaml:"dashboards"`
	EvidenceRetentionDays int                  `json:"evidence_retention_days" yaml:"evidence_retention_days"`
}

type EcosystemSLO struct {
	Name   string `json:"name" yaml:"name"`
	Target string `json:"target" yaml:"target"`
}

type EcosystemAlert struct {
	Name     string `json:"name" yaml:"name"`
	Severity string `json:"severity" yaml:"severity"`
}

type EcosystemDashboard struct {
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url" yaml:"url"`
}
