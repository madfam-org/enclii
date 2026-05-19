package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const (
	// DefaultTunnelCNAME is the legacy fallback tunnel CNAME target.
	// Production routes should use <tunnel-id>.cfargotunnel.com when the
	// Cloudflare client has a configured tunnel ID.
	DefaultTunnelCNAME = "tunnel.enclii.dev"

	// StatusVerified indicates the domain is correctly configured
	StatusVerified = "verified"
	// StatusPending indicates the domain is awaiting DNS configuration
	StatusPending = "pending"
	// StatusMisconfigured indicates the domain DNS is pointing elsewhere
	StatusMisconfigured = "misconfigured"
	// StatusError indicates an error occurred during verification
	StatusError = "error"
)

// TunnelCNAMEForTunnelID returns the Cloudflare Tunnel DNS target.
func TunnelCNAMEForTunnelID(tunnelID string) string {
	if tunnelID == "" {
		return DefaultTunnelCNAME
	}
	return fmt.Sprintf("%s.cfargotunnel.com", tunnelID)
}

// DomainSyncService handles syncing domain status with Cloudflare
type DomainSyncService struct {
	cf          *cloudflare.Client
	repos       *db.Repositories
	logger      *logrus.Logger
	tunnelCNAME string

	// Background sync control
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
}

// NewDomainSyncService creates a new domain sync service
func NewDomainSyncService(
	cfClient *cloudflare.Client,
	repos *db.Repositories,
	logger *logrus.Logger,
) *DomainSyncService {
	tunnelCNAME := DefaultTunnelCNAME
	if cfClient != nil {
		tunnelCNAME = TunnelCNAMEForTunnelID(cfClient.GetTunnelID())
	}

	return &DomainSyncService{
		cf:          cfClient,
		repos:       repos,
		logger:      logger,
		tunnelCNAME: tunnelCNAME,
		stopChan:    make(chan struct{}),
	}
}

// SetTunnelCNAME sets a custom tunnel CNAME target
func (s *DomainSyncService) SetTunnelCNAME(cname string) {
	s.tunnelCNAME = cname
}

// TunnelCNAME returns the configured DNS target for Cloudflare Tunnel CNAMEs.
func (s *DomainSyncService) TunnelCNAME() string {
	if s == nil || s.tunnelCNAME == "" {
		return DefaultTunnelCNAME
	}
	return s.tunnelCNAME
}

// GetCloudflareClient returns the underlying Cloudflare client for DNS operations
func (s *DomainSyncService) GetCloudflareClient() *cloudflare.Client {
	return s.cf
}

// SyncDomainResult contains the result of syncing a single domain
type SyncDomainResult struct {
	DomainID    uuid.UUID `json:"domain_id"`
	Domain      string    `json:"domain"`
	OldStatus   string    `json:"old_status"`
	NewStatus   string    `json:"new_status"`
	DNSVerified bool      `json:"dns_verified"`
	TLSEnabled  bool      `json:"tls_enabled"`
	Error       string    `json:"error,omitempty"`
}

// SyncAllResult contains the result of syncing all domains
type SyncAllResult struct {
	TotalDomains  int                      `json:"total_domains"`
	SyncedDomains int                      `json:"synced_domains"`
	FailedDomains int                      `json:"failed_domains"`
	Results       []SyncDomainResult       `json:"results"`
	TunnelStatus  *cloudflare.TunnelStatus `json:"tunnel_status,omitempty"`
	SyncedAt      time.Time                `json:"synced_at"`
}

// SyncDomain syncs a single domain's status with Cloudflare
func (s *DomainSyncService) SyncDomain(ctx context.Context, domainID uuid.UUID) (*SyncDomainResult, error) {
	// Get domain from database
	domain, err := s.repos.CustomDomains.GetByID(ctx, domainID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get domain: %w", err)
	}

	result := &SyncDomainResult{
		DomainID:  domainID,
		Domain:    domain.Domain,
		OldStatus: s.getStatus(domain),
	}

	// Verify DNS configuration
	dnsResult, err := s.cf.VerifyDomainDNS(ctx, domain.Domain, s.tunnelCNAME)
	if err != nil {
		result.Error = fmt.Sprintf("DNS verification failed: %v", err)
		result.NewStatus = StatusError
		s.logger.WithError(err).WithField("domain", domain.Domain).Warn("Failed to verify domain DNS")
	} else {
		result.DNSVerified = dnsResult.IsCorrect

		// Determine new status based on DNS
		if dnsResult.IsCorrect {
			result.NewStatus = StatusVerified
		} else if !dnsResult.RecordExists {
			result.NewStatus = StatusPending
		} else {
			result.NewStatus = StatusMisconfigured
		}
	}

	// Check TLS status
	tlsStatus, err := s.cf.GetSSLStatus(ctx, domain.Domain)
	if err != nil {
		s.logger.WithError(err).WithField("domain", domain.Domain).Warn("Failed to get SSL status")
	} else {
		result.TLSEnabled = tlsStatus.HasCertificate && tlsStatus.CertificateStatus == "active"
	}

	// Update domain in database
	now := time.Now()
	domain.Verified = result.DNSVerified
	domain.TLSEnabled = result.TLSEnabled
	domain.Status = result.NewStatus
	if result.DNSVerified && domain.VerifiedAt == nil {
		domain.VerifiedAt = &now
	}

	if err := s.repos.CustomDomains.Update(ctx, domain); err != nil {
		s.logger.WithError(err).WithField("domain", domain.Domain).Error("Failed to update domain status")
		result.Error = fmt.Sprintf("database update failed: %v", err)
	}

	s.logger.WithFields(logrus.Fields{
		"domain":       domain.Domain,
		"old_status":   result.OldStatus,
		"new_status":   result.NewStatus,
		"dns_verified": result.DNSVerified,
		"tls_enabled":  result.TLSEnabled,
	}).Info("Domain status synced")

	return result, nil
}

// SyncAllDomains syncs all domains with Cloudflare
func (s *DomainSyncService) SyncAllDomains(ctx context.Context) (*SyncAllResult, error) {
	// Get all domains
	domains, total, err := s.repos.CustomDomains.ListAll(ctx, nil, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list domains: %w", err)
	}

	result := &SyncAllResult{
		TotalDomains: total,
		Results:      make([]SyncDomainResult, 0, len(domains)),
		SyncedAt:     time.Now(),
	}

	// Get tunnel status
	if s.cf.GetTunnelID() != "" {
		tunnelStatus, err := s.cf.GetTunnelStatus(ctx, "")
		if err != nil {
			s.logger.WithError(err).Warn("Failed to get tunnel status")
		} else {
			result.TunnelStatus = tunnelStatus
		}
	}

	// Sync each domain
	for _, domain := range domains {
		syncResult, err := s.SyncDomain(ctx, domain.ID)
		if err != nil {
			result.FailedDomains++
			result.Results = append(result.Results, SyncDomainResult{
				DomainID: domain.ID,
				Domain:   domain.Domain,
				Error:    err.Error(),
			})
			continue
		}
		result.SyncedDomains++
		result.Results = append(result.Results, *syncResult)
	}

	s.logger.WithFields(logrus.Fields{
		"total":  result.TotalDomains,
		"synced": result.SyncedDomains,
		"failed": result.FailedDomains,
	}).Info("Domain sync completed")

	return result, nil
}

// StartBackgroundSync starts periodic background synchronization
func (s *DomainSyncService) StartBackgroundSync(interval time.Duration) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		s.logger.WithField("interval", interval).Info("Starting background domain sync")

		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				if _, err := s.SyncAllDomains(ctx); err != nil {
					s.logger.WithError(err).Error("Background domain sync failed")
				}
				cancel()
			case <-s.stopChan:
				s.logger.Info("Stopping background domain sync")
				return
			}
		}
	}()
}

// StopBackgroundSync stops the background sync worker
func (s *DomainSyncService) StopBackgroundSync() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopChan)
	s.wg.Wait()
}

// GetTunnelStatus returns the current tunnel status
func (s *DomainSyncService) GetTunnelStatus(ctx context.Context) (*cloudflare.TunnelStatus, error) {
	if s.cf == nil {
		return nil, fmt.Errorf("cloudflare client not configured")
	}
	return s.cf.GetTunnelStatus(ctx, "")
}

// getStatus returns the current status string for a domain
func (s *DomainSyncService) getStatus(domain *types.CustomDomain) string {
	if domain.Verified {
		return StatusVerified
	}
	return StatusPending
}
