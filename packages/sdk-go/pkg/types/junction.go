package types

import (
	"time"

	"github.com/google/uuid"
)

// Junction types for routing, ingress, and certificate management.

// Junction represents a routing/ingress configuration for a service.
type Junction struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	ProjectID uuid.UUID  `json:"project_id" db:"project_id"`
	ServiceID uuid.UUID  `json:"service_id" db:"service_id"`
	Domain    string     `json:"domain" db:"domain"`     // e.g., "api.example.com"
	Path      string     `json:"path" db:"path"`         // e.g., "/api/v1"
	Protocol  string     `json:"protocol" db:"protocol"` // "http", "https", "grpc"
	TLS       *TLSConfig `json:"tls,omitempty" db:"-"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
}

// TLSConfig represents TLS/certificate settings for a junction.
type TLSConfig struct {
	Enabled       bool   `json:"enabled"`
	Issuer        string `json:"issuer"`                // "letsencrypt-prod", "letsencrypt-staging", "custom"
	CertSecret    string `json:"cert_secret,omitempty"` // K8s secret name for custom certs
	MinVersion    string `json:"min_version,omitempty"` // "1.2", "1.3"
	ForceRedirect bool   `json:"force_redirect"`        // HTTP → HTTPS redirect
}
