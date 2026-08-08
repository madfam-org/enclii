package cloudflare

import (
	"fmt"
	"time"
)

// Config holds Cloudflare API client configuration
type Config struct {
	APIToken  string // Cloudflare API token with required permissions
	AccountID string // Cloudflare account ID
	ZoneID    string // Primary zone ID (e.g., enclii.dev zone)
	TunnelID  string // Production tunnel ID
}

// APIResponse wraps all Cloudflare API responses
type APIResponse[T any] struct {
	Success    bool         `json:"success"`
	Errors     []APIError   `json:"errors"`
	Messages   []APIMessage `json:"messages"`
	Result     T            `json:"result"`
	ResultInfo *ResultInfo  `json:"result_info,omitempty"`
}

// APIError represents a Cloudflare API error
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements error so callers can errors.As() a failed request back to
// the Cloudflare error code instead of matching on message text. The rendered
// string is unchanged from the previous fmt.Errorf formatting.
func (e *APIError) Error() string {
	return fmt.Sprintf("cloudflare: API error %d: %s", e.Code, e.Message)
}

// APIMessage represents a Cloudflare API message
type APIMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ResultInfo contains pagination information
type ResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

// DNSRecord represents a Cloudflare DNS record
type DNSRecord struct {
	ID         string    `json:"id"`
	ZoneID     string    `json:"zone_id"`
	ZoneName   string    `json:"zone_name"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`    // A, AAAA, CNAME, TXT, etc.
	Content    string    `json:"content"` // Record value
	Proxied    bool      `json:"proxied"` // Orange cloud enabled
	Proxiable  bool      `json:"proxiable"`
	TTL        int       `json:"ttl"`
	Locked     bool      `json:"locked"`
	Comment    string    `json:"comment,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	CreatedOn  time.Time `json:"created_on"`
	ModifiedOn time.Time `json:"modified_on"`
}

// DNSVerificationResult contains the result of DNS verification
type DNSVerificationResult struct {
	Domain          string `json:"domain"`
	RecordExists    bool   `json:"record_exists"`
	RecordType      string `json:"record_type"`
	RecordContent   string `json:"record_content"`
	ExpectedContent string `json:"expected_content"`
	IsCorrect       bool   `json:"is_correct"`
	Proxied         bool   `json:"proxied"`
}

// Zone represents a Cloudflare zone
type Zone struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Status              string    `json:"status"`
	Paused              bool      `json:"paused"`
	Type                string    `json:"type"`
	DevelopmentMode     int       `json:"development_mode"`
	NameServers         []string  `json:"name_servers"`
	OriginalNameServers []string  `json:"original_name_servers"`
	CreatedOn           time.Time `json:"created_on"`
	ModifiedOn          time.Time `json:"modified_on"`
	ActivatedOn         time.Time `json:"activated_on"`
}

// Tunnel represents a Cloudflare Tunnel
type Tunnel struct {
	ID            string             `json:"id"`
	AccountTag    string             `json:"account_tag"`
	Name          string             `json:"name"`
	Status        string             `json:"status"` // healthy, degraded, down, inactive
	CreatedAt     time.Time          `json:"created_at"`
	DeletedAt     *time.Time         `json:"deleted_at,omitempty"`
	ConnsActiveAt *time.Time         `json:"conns_active_at,omitempty"`
	ConnsTotalAt  *time.Time         `json:"conns_total_at,omitempty"`
	Connections   []TunnelConnection `json:"connections,omitempty"`
	TunnelType    string             `json:"tun_type"`
	RemoteConfig  bool               `json:"remote_config"`
}

// TunnelConnection represents an active tunnel connector
type TunnelConnection struct {
	ID            string    `json:"id"`
	ColoName      string    `json:"colo_name"` // Cloudflare colo (e.g., "DFW")
	IsDeleted     bool      `json:"is_pending_reconnect"`
	ClientID      string    `json:"client_id"`
	ClientVersion string    `json:"client_version"`
	OpenedAt      time.Time `json:"opened_at"`
	OriginIP      string    `json:"origin_ip"`
	UUID          string    `json:"uuid"`
}

// TunnelStatus represents aggregated tunnel health status
type TunnelStatus struct {
	TunnelID         string    `json:"tunnel_id"`
	TunnelName       string    `json:"tunnel_name"`
	Status           string    `json:"status"` // active, degraded, inactive
	ActiveConnectors int       `json:"active_connectors"`
	TotalConnectors  int       `json:"total_connectors"`
	LastHealthy      time.Time `json:"last_healthy"`
	Colos            []string  `json:"colos"` // Connected colos
}

// SSLCertificatePack represents an SSL certificate pack
type SSLCertificatePack struct {
	ID                   string    `json:"id"`
	Type                 string    `json:"type"` // universal, advanced
	Hosts                []string  `json:"hosts"`
	Status               string    `json:"status"` // active, pending_validation, etc.
	ValidationMethod     string    `json:"validation_method"`
	ValidityDays         int       `json:"validity_days"`
	CertificateAuthority string    `json:"certificate_authority"`
	CreatedOn            time.Time `json:"created_on"`
	ExpiresOn            time.Time `json:"expires_on"`
}

// SSLVerificationStatus represents the SSL verification status for a hostname
type SSLVerificationStatus struct {
	Hostname          string `json:"hostname"`
	CertificateStatus string `json:"certificate_status"` // active, pending, none
	ValidationMethod  string `json:"validation_method"`
	BrandCheck        bool   `json:"brand_check"`
	CertPackUUID      string `json:"cert_pack_uuid"`
}

// SSLStatus represents the aggregated SSL status for a domain
type SSLStatus struct {
	Domain            string     `json:"domain"`
	HasCertificate    bool       `json:"has_certificate"`
	CertificateStatus string     `json:"certificate_status"` // active, pending, expired, none
	Issuer            string     `json:"issuer"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	IsCloudflareSSL   bool       `json:"is_cloudflare_ssl"`
}

// DomainSyncResult represents the result of syncing a domain with Cloudflare
type DomainSyncResult struct {
	Domain       string                 `json:"domain"`
	DNS          *DNSVerificationResult `json:"dns"`
	SSL          *SSLStatus             `json:"ssl"`
	Tunnel       *TunnelStatus          `json:"tunnel,omitempty"`
	SyncedAt     time.Time              `json:"synced_at"`
	Status       string                 `json:"status"` // verified, pending, misconfigured, error
	ErrorMessage string                 `json:"error_message,omitempty"`
}

// TunnelConfiguration represents a remotely-managed tunnel configuration
type TunnelConfiguration struct {
	Config TunnelConfigData `json:"config"`
}

// TunnelConfigData contains the actual tunnel configuration data
type TunnelConfigData struct {
	Ingress     []TunnelIngressRule `json:"ingress"`
	WarpRouting *WarpRoutingConfig  `json:"warp-routing,omitempty"`
}

// TunnelIngressRule represents an ingress rule in the tunnel configuration
type TunnelIngressRule struct {
	Hostname      string               `json:"hostname,omitempty"`
	Path          string               `json:"path,omitempty"`
	Service       string               `json:"service"`
	OriginRequest *TunnelOriginRequest `json:"originRequest,omitempty"`
}

// TunnelOriginRequest contains origin-specific configuration
type TunnelOriginRequest struct {
	ConnectTimeout   string `json:"connectTimeout,omitempty"`
	KeepAliveTimeout string `json:"keepAliveTimeout,omitempty"`
	NoTLSVerify      bool   `json:"noTLSVerify,omitempty"`
	HTTPHostHeader   string `json:"httpHostHeader,omitempty"`
}

// WarpRoutingConfig contains WARP routing configuration
type WarpRoutingConfig struct {
	Enabled bool `json:"enabled"`
}
