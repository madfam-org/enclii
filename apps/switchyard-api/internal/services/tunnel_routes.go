package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

const (
	// DefaultConfigMapNamespace is the namespace where cloudflared config lives
	DefaultConfigMapNamespace = "cloudflare-tunnel"
	// DefaultConfigMapName is the name of the cloudflared ConfigMap
	DefaultConfigMapName = "cloudflared-config"
	// ConfigYAMLKey is the key in the ConfigMap data that contains the config
	ConfigYAMLKey = "config.yaml"
	// DefaultCatchAllService is the catch-all route (must be last)
	DefaultCatchAllService = "http_status:404"
)

// TunnelRoutesManager is the interface for managing tunnel routes
// This allows using either ConfigMap-based or Cloudflare API-based implementations
type TunnelRoutesManager interface {
	// AddRoute adds a new route to the tunnel configuration
	AddRoute(ctx context.Context, spec *RouteSpec) error
	// RemoveRoute removes a route from the tunnel configuration
	RemoveRoute(ctx context.Context, hostname string) error
	// ListRoutes returns all currently configured routes
	ListRoutes(ctx context.Context) ([]IngressRule, error)
	// RouteExists checks if a route exists for the given hostname
	RouteExists(ctx context.Context, hostname string) (bool, error)
}

// CloudflaredConfig represents the cloudflared configuration structure
type CloudflaredConfig struct {
	Tunnel   string        `yaml:"tunnel,omitempty"`
	Metrics  string        `yaml:"metrics,omitempty"`
	LogLevel string        `yaml:"loglevel,omitempty"`
	Ingress  []IngressRule `yaml:"ingress"`
}

// IngressRule represents a single ingress rule in cloudflared config
type IngressRule struct {
	Hostname      string         `yaml:"hostname,omitempty"`
	Path          string         `yaml:"path,omitempty"`
	Service       string         `yaml:"service"`
	OriginRequest *OriginRequest `yaml:"originRequest,omitempty"`
}

// OriginRequest contains origin-specific configuration
type OriginRequest struct {
	ConnectTimeout   string `yaml:"connectTimeout,omitempty"`
	KeepAliveTimeout string `yaml:"keepAliveTimeout,omitempty"`
	NoTLSVerify      bool   `yaml:"noTLSVerify,omitempty"`
	HTTPHostHeader   string `yaml:"httpHostHeader,omitempty"`
}

// RouteSpec defines a route to add to cloudflared
type RouteSpec struct {
	Hostname         string
	ServiceName      string
	ServiceNamespace string
	ServicePort      int
	ConnectTimeout   string
	KeepAliveTimeout string
}

// TunnelRoutesService manages cloudflared tunnel routes via ConfigMap
type TunnelRoutesService struct {
	k8sClient          *k8s.Client
	logger             *logrus.Logger
	configMapNamespace string
	configMapName      string
	mu                 sync.Mutex
}

// NewTunnelRoutesService creates a new tunnel routes service
func NewTunnelRoutesService(
	k8sClient *k8s.Client,
	logger *logrus.Logger,
) *TunnelRoutesService {
	return &TunnelRoutesService{
		k8sClient:          k8sClient,
		logger:             logger,
		configMapNamespace: DefaultConfigMapNamespace,
		configMapName:      DefaultConfigMapName,
	}
}

// SetConfigMapLocation allows overriding the default ConfigMap location
func (s *TunnelRoutesService) SetConfigMapLocation(namespace, name string) {
	s.configMapNamespace = namespace
	s.configMapName = name
}

// AddRoute adds a new route to the cloudflared ConfigMap
func (s *TunnelRoutesService) AddRoute(ctx context.Context, spec *RouteSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithFields(logrus.Fields{
		"hostname": spec.Hostname,
		"service":  fmt.Sprintf("%s.%s.svc.cluster.local:%d", spec.ServiceName, spec.ServiceNamespace, spec.ServicePort),
	}).Info("Adding tunnel route")

	// Get current config
	config, err := s.getConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cloudflared config: %w", err)
	}

	// Check if route already exists. Case-insensitive: a rule stored in another
	// case names the same host, and treating it as absent appends a duplicate
	// rule for one hostname.
	if indexOfHostname(config.Ingress, spec.Hostname) >= 0 {
		s.logger.WithField("hostname", spec.Hostname).Warn("Route already exists, updating")
		return s.updateExistingRoute(ctx, config, spec)
	}

	// Build service URL
	serviceURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		spec.ServiceName, spec.ServiceNamespace, spec.ServicePort)

	// Create new rule, stored canonically so the config converges on one form.
	newRule := IngressRule{
		Hostname: CanonicalHostname(spec.Hostname),
		Service:  serviceURL,
	}

	// Add origin request config if timeouts specified
	if spec.ConnectTimeout != "" || spec.KeepAliveTimeout != "" {
		newRule.OriginRequest = &OriginRequest{
			ConnectTimeout:   spec.ConnectTimeout,
			KeepAliveTimeout: spec.KeepAliveTimeout,
		}
	}

	// Insert before the catch-all rule (which must be last)
	config.Ingress = insertBeforeCatchAll(config.Ingress, newRule)

	// Save updated config
	if err := s.saveConfig(ctx, config); err != nil {
		return fmt.Errorf("failed to save cloudflared config: %w", err)
	}

	// Trigger cloudflared restart
	if err := s.restartCloudflared(ctx); err != nil {
		s.logger.WithError(err).Warn("Failed to restart cloudflared, config will apply on next restart")
		// Don't fail the operation - the config is saved and will apply on next restart
	}

	s.logger.WithField("hostname", spec.Hostname).Info("Tunnel route added successfully")
	return nil
}

// RemoveRoute removes a route from the cloudflared ConfigMap
func (s *TunnelRoutesService) RemoveRoute(ctx context.Context, hostname string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithField("hostname", hostname).Info("Removing tunnel route")

	// Get current config
	config, err := s.getConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cloudflared config: %w", err)
	}

	// Find and remove the route. Case-insensitive: a byte-exact match left a
	// mixed-case rule in place while reporting the route removed, so the
	// hostname kept resolving to a service that had been torn down.
	newIngress, found := filterOutHostname(config.Ingress, hostname)

	if !found {
		s.logger.WithField("hostname", hostname).Warn("Route not found, nothing to remove")
		return nil
	}

	config.Ingress = newIngress

	// Save updated config
	if err := s.saveConfig(ctx, config); err != nil {
		return fmt.Errorf("failed to save cloudflared config: %w", err)
	}

	// Trigger cloudflared restart
	if err := s.restartCloudflared(ctx); err != nil {
		s.logger.WithError(err).Warn("Failed to restart cloudflared, config will apply on next restart")
	}

	s.logger.WithField("hostname", hostname).Info("Tunnel route removed successfully")
	return nil
}

// ListRoutes returns all currently configured routes
func (s *TunnelRoutesService) ListRoutes(ctx context.Context) ([]IngressRule, error) {
	config, err := s.getConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cloudflared config: %w", err)
	}

	// Filter out catch-all rule
	routes := make([]IngressRule, 0, len(config.Ingress))
	for _, rule := range config.Ingress {
		if rule.Hostname != "" {
			routes = append(routes, rule)
		}
	}

	return routes, nil
}

// RouteExists checks if a route exists for the given hostname
func (s *TunnelRoutesService) RouteExists(ctx context.Context, hostname string) (bool, error) {
	config, err := s.getConfig(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get cloudflared config: %w", err)
	}

	return indexOfHostname(config.Ingress, hostname) >= 0, nil
}

// getConfig retrieves and parses the cloudflared ConfigMap
func (s *TunnelRoutesService) getConfig(ctx context.Context) (*CloudflaredConfig, error) {
	cm, err := s.k8sClient.GetConfigMap(ctx, s.configMapNamespace, s.configMapName)
	if err != nil {
		return nil, err
	}

	configYAML, ok := cm.Data[ConfigYAMLKey]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %s/%s missing key %s", s.configMapNamespace, s.configMapName, ConfigYAMLKey)
	}

	var config CloudflaredConfig
	if err := yaml.Unmarshal([]byte(configYAML), &config); err != nil {
		return nil, fmt.Errorf("failed to parse cloudflared config YAML: %w", err)
	}

	return &config, nil
}

// saveConfig saves the cloudflared config back to the ConfigMap
func (s *TunnelRoutesService) saveConfig(ctx context.Context, config *CloudflaredConfig) error {
	// Get existing ConfigMap
	cm, err := s.k8sClient.GetConfigMap(ctx, s.configMapNamespace, s.configMapName)
	if err != nil {
		return err
	}

	// Serialize config to YAML
	configYAML, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize cloudflared config: %w", err)
	}

	// Update ConfigMap data
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[ConfigYAMLKey] = string(configYAML)

	// Add annotation for change tracking
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	cm.Annotations["enclii.dev/last-modified"] = time.Now().Format(time.RFC3339)
	cm.Annotations["enclii.dev/modified-by"] = "tunnel-routes-service"

	// Save ConfigMap
	if _, err := s.k8sClient.UpdateConfigMap(ctx, cm); err != nil {
		return err
	}

	return nil
}

// updateExistingRoute updates an existing route in place
func (s *TunnelRoutesService) updateExistingRoute(ctx context.Context, config *CloudflaredConfig, spec *RouteSpec) error {
	serviceURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		spec.ServiceName, spec.ServiceNamespace, spec.ServicePort)

	for i, rule := range config.Ingress {
		if sameHostname(rule.Hostname, spec.Hostname) {
			// Canonicalise on the way past: this is the reconciliation that
			// converts a pre-existing mixed-case rule, which is why the tunnel
			// config needs no migration of its own.
			config.Ingress[i].Hostname = CanonicalHostname(rule.Hostname)
			config.Ingress[i].Service = serviceURL
			if spec.ConnectTimeout != "" || spec.KeepAliveTimeout != "" {
				config.Ingress[i].OriginRequest = &OriginRequest{
					ConnectTimeout:   spec.ConnectTimeout,
					KeepAliveTimeout: spec.KeepAliveTimeout,
				}
			}
			break
		}
	}

	if err := s.saveConfig(ctx, config); err != nil {
		return fmt.Errorf("failed to save updated cloudflared config: %w", err)
	}

	// Trigger cloudflared restart
	if err := s.restartCloudflared(ctx); err != nil {
		s.logger.WithError(err).Warn("Failed to restart cloudflared, config will apply on next restart")
	}

	return nil
}

// restartCloudflared triggers a rolling restart of cloudflared pods
func (s *TunnelRoutesService) restartCloudflared(ctx context.Context) error {
	return s.k8sClient.RollingRestart(ctx, s.configMapNamespace, "cloudflared")
}

// insertBeforeCatchAll inserts a rule before the catch-all rule
// indexOfHostname returns the index of the rule serving hostname, or -1.
//
// Case-insensitive, and that is the whole point: the ingress config is the one
// hostname-keyed store no migration can rewrite, so the comparison has to
// absorb a mixed-case rule rather than expecting the data to be clean.
func indexOfHostname(rules []IngressRule, hostname string) int {
	for i, rule := range rules {
		if sameHostname(rule.Hostname, hostname) {
			return i
		}
	}
	return -1
}

// filterOutHostname returns rules with every rule serving hostname removed,
// and whether any were.
func filterOutHostname(rules []IngressRule, hostname string) ([]IngressRule, bool) {
	kept := make([]IngressRule, 0, len(rules))
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

func insertBeforeCatchAll(rules []IngressRule, newRule IngressRule) []IngressRule {
	// Find catch-all rule (rule without hostname)
	catchAllIndex := -1
	for i, rule := range rules {
		if rule.Hostname == "" && isCatchAllService(rule.Service) {
			catchAllIndex = i
			break
		}
	}

	// If no catch-all found, append and add default catch-all
	if catchAllIndex == -1 {
		rules = append(rules, newRule)
		rules = append(rules, IngressRule{Service: DefaultCatchAllService})
		return rules
	}

	// Insert before catch-all
	result := make([]IngressRule, 0, len(rules)+1)
	result = append(result, rules[:catchAllIndex]...)
	result = append(result, newRule)
	result = append(result, rules[catchAllIndex:]...)
	return result
}

// isCatchAllService checks if a service is a catch-all service
func isCatchAllService(service string) bool {
	return service == DefaultCatchAllService ||
		strings.HasPrefix(service, "http_status:") ||
		service == "http://localhost:8080" // Common catch-all pattern
}
