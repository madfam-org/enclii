package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// PostgresProvisioner handles PostgreSQL database provisioning via CloudNativePG
type PostgresProvisioner struct {
	k8sClient     *k8s.Client
	dynamicClient dynamic.Interface
	logger        *logrus.Logger
}

// CloudNativePG Group Version Resource
var cnpgGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "clusters",
}

// NewPostgresProvisioner creates a new PostgreSQL provisioner
func NewPostgresProvisioner(k8sClient *k8s.Client, logger *logrus.Logger) *PostgresProvisioner {
	// Create dynamic client for CRD operations
	dynamicClient, err := dynamic.NewForConfig(k8sClient.Config())
	if err != nil {
		logger.WithError(err).Error("Failed to create dynamic client - CloudNativePG operations will fail")
	}

	return &PostgresProvisioner{
		k8sClient:     k8sClient,
		dynamicClient: dynamicClient,
		logger:        logger,
	}
}

// Provision creates a new PostgreSQL cluster via CloudNativePG
func (p *PostgresProvisioner) Provision(ctx context.Context, req *ProvisionRequest) (*ProvisionResult, error) {
	logger := p.logger.WithFields(logrus.Fields{
		"addon_id":  req.Addon.ID,
		"namespace": req.Namespace,
		"name":      req.Addon.Name,
	})

	logger.Info("Provisioning PostgreSQL cluster")

	// Ensure namespace exists
	if err := p.k8sClient.EnsureNamespace(ctx, req.Namespace); err != nil {
		return nil, fmt.Errorf("failed to ensure namespace: %w", err)
	}
	// Stamp the owning project on the namespace so the per-project ingress
	// policy below can select same-project namespaces (2026-08-17 audit: the
	// flat data-access label was the ONLY isolation key and let any tenant
	// reach any addon DB port). Non-fatal: log and continue — but this is the
	// key the network isolation depends on.
	if err := p.k8sClient.LabelProjectOnNamespace(ctx, req.Namespace, req.ProjectID.String()); err != nil {
		logger.WithError(err).Error("Failed to label namespace with project — per-project DB network isolation may be incomplete")
	}
	// Bound the addon namespace: a LimitRange floor so a cluster with empty
	// CPU/Memory (legacy/shared-discovered plan) is not BestEffort, and a quota
	// ceiling so it cannot starve the node (2026-08-17 audit #7). Non-fatal.
	if err := p.k8sClient.EnsureAddonNamespaceResourceGuards(ctx, req.Namespace); err != nil {
		logger.WithError(err).Error("Failed to apply addon namespace resource guards — the DB may be unbounded")
	}

	// Generate resource name
	resourceName := fmt.Sprintf("pg-%s-%s", req.Addon.Name, req.Addon.ID.String()[:8])

	// Build CloudNativePG Cluster manifest
	cluster := p.buildClusterManifest(req, resourceName)

	// Convert to unstructured
	clusterJSON, err := json.Marshal(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cluster spec: %w", err)
	}

	var unstructuredObj unstructured.Unstructured
	if err := json.Unmarshal(clusterJSON, &unstructuredObj.Object); err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	// Create the CloudNativePG Cluster
	_, err = p.dynamicClient.Resource(cnpgGVR).Namespace(req.Namespace).Create(
		ctx,
		&unstructuredObj,
		metav1.CreateOptions{},
	)
	if err != nil {
		logger.WithError(err).Error("Failed to create CloudNativePG cluster")
		return nil, fmt.Errorf("failed to create PostgreSQL cluster: %w", err)
	}

	// Open the addon namespace's default-deny to consuming services. Every addon
	// namespace gets a default-deny NetworkPolicy (EnsureNamespace); without a
	// companion allow-rule the CNPG pods are unreachable from the services that
	// need them (the eido-db go-live crash-looped for hours on a silent
	// ConnectionRefused for exactly this reason). Scope the allow to this
	// cluster's pods on 5432, from any namespace labeled data-access.
	// PROJECT-SCOPED ingress, not the flat data-access class: only namespaces
	// carrying THIS project's label may reach the cluster on 5432. This is the
	// tenant-isolation fix — a self-checkout tenant's pod can no longer open a
	// connection to another tenant's clinical Postgres port; credential
	// separation is the second layer, not the only one (2026-08-17 audit #2).
	if npErr := p.k8sClient.EnsureProjectScopedIngressPolicy(
		ctx, req.Namespace, fmt.Sprintf("%s-data-access", resourceName),
		req.ProjectID.String(), map[string]string{"cnpg.io/cluster": resourceName}, 5432,
	); npErr != nil {
		// Non-fatal: the cluster is up; surface the gap so the operator can add
		// the policy rather than leaving the addon silently unreachable.
		logger.WithError(npErr).Error("Failed to create project-scoped ingress NetworkPolicy — consumers may be blocked by default-deny")
	}

	// Connection secret name follows CloudNativePG naming convention
	connectionSecret := fmt.Sprintf("%s-app", resourceName)

	logger.Info("PostgreSQL cluster created successfully")

	return &ProvisionResult{
		K8sResourceName:  resourceName,
		ConnectionSecret: connectionSecret,
		Message:          "PostgreSQL cluster creation initiated",
	}, nil
}

// buildClusterManifest builds the CloudNativePG Cluster manifest
func (p *PostgresProvisioner) buildClusterManifest(req *ProvisionRequest, resourceName string) map[string]interface{} {
	config := req.Addon.Config

	// Determine instances (default to 1 for non-HA)
	instances := config.Replicas
	if instances == 0 {
		instances = DefaultInstances
	}
	if config.HAEnabled && instances < 3 {
		instances = 3 // Minimum 3 for HA
	}

	// Parse PostgreSQL version
	postgresVersion := DefaultPostgresVersion
	if config.Version != "" {
		if v, err := strconv.Atoi(config.Version); err == nil {
			postgresVersion = v
		}
	}

	// Determine storage size
	storageSize := fmt.Sprintf("%dGi", config.StorageGB)
	if config.StorageGB == 0 {
		storageSize = DefaultStorageSize
	}

	// Build the cluster spec
	cluster := map[string]interface{}{
		"apiVersion": CloudNativePGAPIVersion,
		"kind":       CloudNativePGKind,
		"metadata": map[string]interface{}{
			"name":      resourceName,
			"namespace": req.Namespace,
			"labels": map[string]interface{}{
				LabelManagedBy: LabelManagedValue,
				LabelAddonID:   req.Addon.ID.String(),
				LabelProjectID: req.ProjectID.String(),
				LabelAddonType: string(types.DatabaseAddonTypePostgres),
			},
		},
		"spec": map[string]interface{}{
			"instances":             instances,
			"postgresVersion":       postgresVersion,
			"primaryUpdateStrategy": "unsupervised",
			"storage": map[string]interface{}{
				"size": storageSize,
			},
			"bootstrap": map[string]interface{}{
				"initdb": map[string]interface{}{
					"database": DefaultDatabase,
					"owner":    DefaultUser,
				},
			},
		},
	}

	// Add resource limits if specified
	spec := cluster["spec"].(map[string]interface{})
	if config.CPU != "" || config.Memory != "" {
		resources := map[string]interface{}{}
		requests := map[string]interface{}{}
		limits := map[string]interface{}{}

		if config.CPU != "" {
			requests["cpu"] = config.CPU
			limits["cpu"] = config.CPU
		}
		if config.Memory != "" {
			requests["memory"] = config.Memory
			limits["memory"] = config.Memory
		}

		resources["requests"] = requests
		resources["limits"] = limits
		spec["resources"] = resources
	}

	// Add monitoring if available
	spec["monitoring"] = map[string]interface{}{
		// Deep DB metrics (pg_up, connections, replication lag) need a PodMonitor
		// selector that covers project-* namespaces plus a postgres-exporter per
		// addon; that Prometheus-side wiring is a tracked follow-up (2026-08-17
		// audit #4). Until it lands, pod/deploy/backup ALERTS for client DBs come
		// from the cluster-wide kube-state-metrics rules (rescoped in
		// prometheus-platform-rules.yaml), so a crashlooping/failed-backup client
		// DB now pages even without the exporter.
		"enablePodMonitor": false,
	}

	return cluster
}

// Deprovision removes a PostgreSQL cluster
func (p *PostgresProvisioner) Deprovision(ctx context.Context, addon *types.DatabaseAddon) error {
	logger := p.logger.WithFields(logrus.Fields{
		"addon_id":  addon.ID,
		"namespace": addon.K8sNamespace,
		"resource":  addon.K8sResourceName,
	})

	logger.Info("Deprovisioning PostgreSQL cluster")

	if addon.K8sNamespace == "" || addon.K8sResourceName == "" {
		logger.Warn("No K8s resource info found, skipping deprovision")
		return nil
	}

	// Delete the CloudNativePG Cluster
	err := p.dynamicClient.Resource(cnpgGVR).Namespace(addon.K8sNamespace).Delete(
		ctx,
		addon.K8sResourceName,
		metav1.DeleteOptions{},
	)
	if err != nil {
		// If not found, consider it already deleted
		if isNotFoundError(err) {
			logger.Info("PostgreSQL cluster already deleted")
			return nil
		}
		return fmt.Errorf("failed to delete PostgreSQL cluster: %w", err)
	}

	logger.Info("PostgreSQL cluster deleted successfully")
	return nil
}

// GetStatus checks the current status of a PostgreSQL cluster
func (p *PostgresProvisioner) GetStatus(ctx context.Context, addon *types.DatabaseAddon) (*StatusResult, error) {
	if addon.K8sNamespace == "" || addon.K8sResourceName == "" {
		return &StatusResult{
			Status:        types.DatabaseAddonStatusPending,
			StatusMessage: "Waiting for provisioning",
		}, nil
	}

	// Get the CloudNativePG Cluster
	cluster, err := p.dynamicClient.Resource(cnpgGVR).Namespace(addon.K8sNamespace).Get(
		ctx,
		addon.K8sResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		if isNotFoundError(err) {
			return &StatusResult{
				Status:        types.DatabaseAddonStatusFailed,
				StatusMessage: "PostgreSQL cluster not found",
			}, nil
		}
		return nil, fmt.Errorf("failed to get cluster status: %w", err)
	}

	// Extract status from the cluster object
	return p.parseClusterStatus(cluster, addon)
}

// parseClusterStatus extracts status information from a CloudNativePG Cluster
func (p *PostgresProvisioner) parseClusterStatus(cluster *unstructured.Unstructured, addon *types.DatabaseAddon) (*StatusResult, error) {
	status, found, err := unstructured.NestedMap(cluster.Object, "status")
	if err != nil || !found {
		return &StatusResult{
			Status:        types.DatabaseAddonStatusProvisioning,
			StatusMessage: "Cluster status not yet available",
		}, nil
	}

	// Check readyInstances
	readyInstances, _, _ := unstructured.NestedInt64(status, "readyInstances")
	instances, _, _ := unstructured.NestedInt64(status, "instances")

	// Get phase
	phase, _, _ := unstructured.NestedString(status, "phase")

	result := &StatusResult{
		DatabaseName: DefaultDatabase,
		Username:     DefaultUser,
	}

	// Determine status based on phase and ready instances
	switch phase {
	case "Cluster in healthy state":
		if readyInstances > 0 {
			result.Status = types.DatabaseAddonStatusReady
			result.StatusMessage = fmt.Sprintf("Cluster ready with %d/%d instances", readyInstances, instances)
			result.Ready = true
		} else {
			result.Status = types.DatabaseAddonStatusProvisioning
			result.StatusMessage = "Cluster healthy but no ready instances"
		}
	case "Setting up primary":
		result.Status = types.DatabaseAddonStatusProvisioning
		result.StatusMessage = "Setting up primary instance"
	case "Creating replica":
		result.Status = types.DatabaseAddonStatusProvisioning
		result.StatusMessage = "Creating replica instances"
	case "Failed":
		result.Status = types.DatabaseAddonStatusFailed
		result.StatusMessage = "Cluster provisioning failed"
	default:
		if readyInstances > 0 && readyInstances == instances {
			result.Status = types.DatabaseAddonStatusReady
			result.StatusMessage = fmt.Sprintf("Cluster ready (%s)", phase)
			result.Ready = true
		} else {
			result.Status = types.DatabaseAddonStatusProvisioning
			result.StatusMessage = fmt.Sprintf("Provisioning (%s)", phase)
		}
	}

	// Extract connection info
	writeService, _, _ := unstructured.NestedString(status, "writeService")
	if writeService != "" {
		result.Host = fmt.Sprintf("%s.%s.svc.cluster.local", writeService, addon.K8sNamespace)
		result.Port = 5432
	}

	return result, nil
}

// GetCredentials retrieves connection credentials from K8s secrets
func (p *PostgresProvisioner) GetCredentials(ctx context.Context, addon *types.DatabaseAddon) (*types.DatabaseAddonCredentials, error) {
	if addon.ConnectionSecret == "" || addon.K8sNamespace == "" {
		return nil, fmt.Errorf("addon does not have connection secret configured")
	}

	// Get the secret
	secret, err := p.k8sClient.Clientset.CoreV1().Secrets(addon.K8sNamespace).Get(
		ctx,
		addon.ConnectionSecret,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection secret: %w", err)
	}

	// Extract credentials from secret
	// CloudNativePG secrets contain: username, password, host, port, dbname, uri
	creds := &types.DatabaseAddonCredentials{
		Host:         string(secret.Data["host"]),
		DatabaseName: string(secret.Data["dbname"]),
		Username:     string(secret.Data["username"]),
		Password:     string(secret.Data["password"]),
	}

	// Parse port
	if portStr := string(secret.Data["port"]); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			creds.Port = port
		}
	}
	if creds.Port == 0 {
		creds.Port = 5432
	}

	// Get connection URI if available, otherwise build it
	if uri := string(secret.Data["uri"]); uri != "" {
		creds.ConnectionURI = uri
	} else {
		creds.ConnectionURI = fmt.Sprintf(
			"postgresql://%s:%s@%s:%d/%s?sslmode=require",
			creds.Username,
			creds.Password,
			creds.Host,
			creds.Port,
			creds.DatabaseName,
		)
	}

	return creds, nil
}

// GetConnectionURI builds the connection URI for the addon
func (p *PostgresProvisioner) GetConnectionURI(ctx context.Context, addon *types.DatabaseAddon) (string, error) {
	creds, err := p.GetCredentials(ctx, addon)
	if err != nil {
		return "", err
	}
	return creds.ConnectionURI, nil
}

// isNotFoundError checks if an error is a "not found" error
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check for various not found error patterns
	errStr := err.Error()
	return contains(errStr, "not found") || contains(errStr, "NotFound")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
