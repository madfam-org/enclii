package services

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
)

// TunnelRoutesServiceCloudflare manages tunnel routes via Cloudflare API
// This is used for remotely-managed tunnels (configured via Cloudflare dashboard/API)
type TunnelRoutesServiceCloudflare struct {
	cfClient *cloudflare.Client
	logger   *logrus.Logger
	tunnelID string
	mu       sync.Mutex
}

// NewTunnelRoutesServiceCloudflare creates a new Cloudflare API-based tunnel routes service
func NewTunnelRoutesServiceCloudflare(
	cfClient *cloudflare.Client,
	logger *logrus.Logger,
) *TunnelRoutesServiceCloudflare {
	return &TunnelRoutesServiceCloudflare{
		cfClient: cfClient,
		logger:   logger,
		tunnelID: cfClient.GetTunnelID(),
	}
}

// SetTunnelID allows overriding the default tunnel ID
func (s *TunnelRoutesServiceCloudflare) SetTunnelID(tunnelID string) {
	s.tunnelID = tunnelID
}

// AddRoute adds a new route to the tunnel configuration via Cloudflare API
func (s *TunnelRoutesServiceCloudflare) AddRoute(ctx context.Context, spec *RouteSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithFields(logrus.Fields{
		"hostname": spec.Hostname,
		"service":  fmt.Sprintf("%s.%s.svc.cluster.local:%d", spec.ServiceName, spec.ServiceNamespace, spec.ServicePort),
	}).Info("Adding tunnel route via Cloudflare API")

	// Get current configuration
	config, err := s.cfClient.GetTunnelConfiguration(ctx, s.tunnelID)
	if err != nil {
		return fmt.Errorf("failed to get tunnel configuration: %w", err)
	}

	// Check if route already exists. Case-insensitive: see CanonicalHostname.
	if i := indexOfHostnameCF(config.Config.Ingress, spec.Hostname); i >= 0 {
		s.logger.WithField("hostname", spec.Hostname).Warn("Route already exists, updating")
		return s.updateExistingRoute(ctx, config, i, spec)
	}

	// Build service URL
	serviceURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		spec.ServiceName, spec.ServiceNamespace, spec.ServicePort)

	// Create new rule, stored canonically so the config converges on one form.
	newRule := cloudflare.TunnelIngressRule{
		Hostname: CanonicalHostname(spec.Hostname),
		Service:  serviceURL,
	}

	// Add origin request config if timeouts specified
	if spec.ConnectTimeout != "" || spec.KeepAliveTimeout != "" {
		newRule.OriginRequest = &cloudflare.TunnelOriginRequest{
			ConnectTimeout:   spec.ConnectTimeout,
			KeepAliveTimeout: spec.KeepAliveTimeout,
		}
	}

	// Insert before the catch-all rule (which must be last)
	config.Config.Ingress = insertBeforeCatchAllCF(config.Config.Ingress, newRule)

	// Update configuration via API
	if err := s.cfClient.UpdateTunnelConfiguration(ctx, s.tunnelID, config); err != nil {
		return fmt.Errorf("failed to update tunnel configuration: %w", err)
	}

	s.logger.WithField("hostname", spec.Hostname).Info("Tunnel route added successfully via Cloudflare API")
	return nil
}

// RemoveRoute removes a route from the tunnel configuration via Cloudflare API
func (s *TunnelRoutesServiceCloudflare) RemoveRoute(ctx context.Context, hostname string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithField("hostname", hostname).Info("Removing tunnel route via Cloudflare API")

	// Get current configuration
	config, err := s.cfClient.GetTunnelConfiguration(ctx, s.tunnelID)
	if err != nil {
		return fmt.Errorf("failed to get tunnel configuration: %w", err)
	}

	// Find and remove the route
	newIngress, found := filterOutHostnameCF(config.Config.Ingress, hostname)

	if !found {
		s.logger.WithField("hostname", hostname).Warn("Route not found, nothing to remove")
		return nil
	}

	config.Config.Ingress = newIngress

	// Update configuration via API
	if err := s.cfClient.UpdateTunnelConfiguration(ctx, s.tunnelID, config); err != nil {
		return fmt.Errorf("failed to update tunnel configuration: %w", err)
	}

	s.logger.WithField("hostname", hostname).Info("Tunnel route removed successfully via Cloudflare API")
	return nil
}

// ListRoutes returns all currently configured routes
func (s *TunnelRoutesServiceCloudflare) ListRoutes(ctx context.Context) ([]IngressRule, error) {
	config, err := s.cfClient.GetTunnelConfiguration(ctx, s.tunnelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tunnel configuration: %w", err)
	}

	// Convert Cloudflare rules to our IngressRule format and filter out catch-all
	routes := make([]IngressRule, 0, len(config.Config.Ingress))
	for _, rule := range config.Config.Ingress {
		if rule.Hostname != "" {
			ingressRule := IngressRule{
				Hostname: rule.Hostname,
				Path:     rule.Path,
				Service:  rule.Service,
			}
			if rule.OriginRequest != nil {
				ingressRule.OriginRequest = &OriginRequest{
					ConnectTimeout:   rule.OriginRequest.ConnectTimeout,
					KeepAliveTimeout: rule.OriginRequest.KeepAliveTimeout,
					NoTLSVerify:      rule.OriginRequest.NoTLSVerify,
					HTTPHostHeader:   rule.OriginRequest.HTTPHostHeader,
				}
			}
			routes = append(routes, ingressRule)
		}
	}

	return routes, nil
}

// RouteExists checks if a route exists for the given hostname
func (s *TunnelRoutesServiceCloudflare) RouteExists(ctx context.Context, hostname string) (bool, error) {
	config, err := s.cfClient.GetTunnelConfiguration(ctx, s.tunnelID)
	if err != nil {
		return false, fmt.Errorf("failed to get tunnel configuration: %w", err)
	}

	return indexOfHostnameCF(config.Config.Ingress, hostname) >= 0, nil
}

// updateExistingRoute updates an existing route in place
func (s *TunnelRoutesServiceCloudflare) updateExistingRoute(ctx context.Context, config *cloudflare.TunnelConfiguration, index int, spec *RouteSpec) error {
	serviceURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		spec.ServiceName, spec.ServiceNamespace, spec.ServicePort)

	// Canonicalise on the way past, so a pre-existing mixed-case rule is
	// converted by the first reconciliation that touches it.
	config.Config.Ingress[index].Hostname = CanonicalHostname(config.Config.Ingress[index].Hostname)
	config.Config.Ingress[index].Service = serviceURL
	if spec.ConnectTimeout != "" || spec.KeepAliveTimeout != "" {
		config.Config.Ingress[index].OriginRequest = &cloudflare.TunnelOriginRequest{
			ConnectTimeout:   spec.ConnectTimeout,
			KeepAliveTimeout: spec.KeepAliveTimeout,
		}
	}

	if err := s.cfClient.UpdateTunnelConfiguration(ctx, s.tunnelID, config); err != nil {
		return fmt.Errorf("failed to update tunnel configuration: %w", err)
	}

	return nil
}

// insertBeforeCatchAllCF inserts a rule before the catch-all rule (Cloudflare types)
// indexOfHostnameCF returns the index of the rule serving hostname, or -1.
// Case-insensitive; see indexOfHostname.
func indexOfHostnameCF(rules []cloudflare.TunnelIngressRule, hostname string) int {
	for i, rule := range rules {
		if sameHostname(rule.Hostname, hostname) {
			return i
		}
	}
	return -1
}

// filterOutHostnameCF returns rules with every rule serving hostname removed,
// and whether any were.
func filterOutHostnameCF(
	rules []cloudflare.TunnelIngressRule, hostname string,
) ([]cloudflare.TunnelIngressRule, bool) {
	kept := make([]cloudflare.TunnelIngressRule, 0, len(rules))
	found := false
	for _, rule := range rules {
		if sameHostname(rule.Hostname, hostname) {
			found = true
			continue
		}
		kept = append(kept, rule)
	}
	return kept, found
}

func insertBeforeCatchAllCF(rules []cloudflare.TunnelIngressRule, newRule cloudflare.TunnelIngressRule) []cloudflare.TunnelIngressRule {
	// Find catch-all rule (rule without hostname)
	catchAllIndex := -1
	for i, rule := range rules {
		if rule.Hostname == "" && isCatchAllServiceCF(rule.Service) {
			catchAllIndex = i
			break
		}
	}

	// If no catch-all found, append and add default catch-all
	if catchAllIndex == -1 {
		rules = append(rules, newRule)
		rules = append(rules, cloudflare.TunnelIngressRule{Service: DefaultCatchAllService})
		return rules
	}

	// Insert before catch-all
	result := make([]cloudflare.TunnelIngressRule, 0, len(rules)+1)
	result = append(result, rules[:catchAllIndex]...)
	result = append(result, newRule)
	result = append(result, rules[catchAllIndex:]...)
	return result
}

// isCatchAllServiceCF checks if a service is a catch-all service
func isCatchAllServiceCF(service string) bool {
	return service == DefaultCatchAllService ||
		strings.HasPrefix(service, "http_status:") ||
		service == "http://localhost:8080"
}
