package addons

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestConstants(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"CloudNativePGAPIVersion", CloudNativePGAPIVersion, "postgresql.cnpg.io/v1"},
		{"CloudNativePGKind", CloudNativePGKind, "Cluster"},
		{"DefaultStorageSize", DefaultStorageSize, "10Gi"},
		{"DefaultCPU", DefaultCPU, "100m"},
		{"DefaultMemory", DefaultMemory, "256Mi"},
		{"DefaultDatabase", DefaultDatabase, "app"},
		{"DefaultUser", DefaultUser, "app"},
		{"LabelManagedBy", LabelManagedBy, "managed-by"},
		{"LabelAddonID", LabelAddonID, "enclii.dev/addon-id"},
		{"LabelProjectID", LabelProjectID, "enclii.dev/project-id"},
		{"LabelAddonType", LabelAddonType, "enclii.dev/addon-type"},
		{"LabelManagedValue", LabelManagedValue, "enclii"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.value, tt.want)
			}
		})
	}

	if DefaultPostgresVersion != 16 {
		t.Errorf("DefaultPostgresVersion = %d, want 16", DefaultPostgresVersion)
	}
	if DefaultInstances != 1 {
		t.Errorf("DefaultInstances = %d, want 1", DefaultInstances)
	}
}

func TestCloudNativePGCluster_JSON(t *testing.T) {
	cluster := CloudNativePGCluster{
		APIVersion: CloudNativePGAPIVersion,
		Kind:       CloudNativePGKind,
		Metadata: CloudNativePGMetadata{
			Name:      "test-db",
			Namespace: "test-ns",
			Labels: map[string]string{
				LabelManagedBy: LabelManagedValue,
				LabelAddonType: "postgres",
			},
		},
		Spec: CloudNativePGClusterSpec{
			Instances:       DefaultInstances,
			PostgresVersion: DefaultPostgresVersion,
			Storage: CloudNativePGStorage{
				Size: DefaultStorageSize,
			},
			Resources: CloudNativePGResources{
				Requests: CloudNativePGResourceList{
					CPU:    DefaultCPU,
					Memory: DefaultMemory,
				},
			},
			Bootstrap: &CloudNativePGBootstrap{
				InitDB: &CloudNativePGInitDB{
					Database: "mydb",
					Owner:    "myuser",
				},
			},
		},
	}

	data, err := json.Marshal(cluster)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded CloudNativePGCluster
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.APIVersion != CloudNativePGAPIVersion {
		t.Errorf("APIVersion = %q", decoded.APIVersion)
	}
	if decoded.Kind != CloudNativePGKind {
		t.Errorf("Kind = %q", decoded.Kind)
	}
	if decoded.Metadata.Name != "test-db" {
		t.Errorf("Metadata.Name = %q", decoded.Metadata.Name)
	}
	if decoded.Spec.Instances != 1 {
		t.Errorf("Spec.Instances = %d", decoded.Spec.Instances)
	}
	if decoded.Spec.Storage.Size != DefaultStorageSize {
		t.Errorf("Spec.Storage.Size = %q", decoded.Spec.Storage.Size)
	}
	if decoded.Spec.Bootstrap.InitDB.Database != "mydb" {
		t.Errorf("Bootstrap.InitDB.Database = %q", decoded.Spec.Bootstrap.InitDB.Database)
	}
}

func TestProvisionRequest_Fields(t *testing.T) {
	projID := uuid.New()
	req := &ProvisionRequest{
		Namespace: "test-ns",
		ProjectID: projID,
	}

	if req.Namespace != "test-ns" {
		t.Errorf("Namespace = %q", req.Namespace)
	}
	if req.ProjectID != projID {
		t.Errorf("ProjectID = %s", req.ProjectID)
	}
}

func TestProvisionResult_Fields(t *testing.T) {
	result := &ProvisionResult{
		K8sResourceName:  "pg-test-db",
		ConnectionSecret: "pg-test-db-credentials",
		Message:          "Provisioned successfully",
	}

	if result.K8sResourceName != "pg-test-db" {
		t.Errorf("K8sResourceName = %q", result.K8sResourceName)
	}
	if result.ConnectionSecret != "pg-test-db-credentials" {
		t.Errorf("ConnectionSecret = %q", result.ConnectionSecret)
	}
}

func TestStatusResult_Fields(t *testing.T) {
	result := &StatusResult{
		Status:       "ready",
		Host:         "pg-test-db-rw.test-ns.svc.cluster.local",
		Port:         5432,
		DatabaseName: "mydb",
		Username:     "myuser",
		Ready:        true,
	}

	if !result.Ready {
		t.Error("expected Ready=true")
	}
	if result.Port != 5432 {
		t.Errorf("Port = %d", result.Port)
	}
	if result.Host != "pg-test-db-rw.test-ns.svc.cluster.local" {
		t.Errorf("Host = %q", result.Host)
	}
}

func TestCloudNativePGCluster_WithBackup(t *testing.T) {
	cluster := CloudNativePGCluster{
		APIVersion: CloudNativePGAPIVersion,
		Kind:       CloudNativePGKind,
		Metadata: CloudNativePGMetadata{
			Name:      "backed-up-db",
			Namespace: "prod",
		},
		Spec: CloudNativePGClusterSpec{
			Instances: 3,
			Storage:   CloudNativePGStorage{Size: "50Gi", StorageClass: "longhorn"},
			Backup: &CloudNativePGBackupSpec{
				RetentionPolicy: "30d",
				BarmanObjectStore: &CloudNativePGBarmanStore{
					DestinationPath: "s3://backup-bucket/postgres/",
					S3Credentials: &CloudNativePGS3Credentials{
						AccessKeyID:     CloudNativePGSecretKeyRef{Name: "s3-creds", Key: "ACCESS_KEY_ID"},
						SecretAccessKey: CloudNativePGSecretKeyRef{Name: "s3-creds", Key: "SECRET_ACCESS_KEY"},
					},
					Wal:  &CloudNativePGWalConfig{Compression: "gzip"},
					Data: &CloudNativePGDataConfig{Compression: "gzip"},
				},
			},
		},
	}

	data, err := json.Marshal(cluster)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded CloudNativePGCluster
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Spec.Backup == nil {
		t.Fatal("expected non-nil Backup")
	}
	if decoded.Spec.Backup.RetentionPolicy != "30d" {
		t.Errorf("RetentionPolicy = %q", decoded.Spec.Backup.RetentionPolicy)
	}
	if decoded.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyID.Key != "ACCESS_KEY_ID" {
		t.Errorf("AccessKeyID.Key = %q", decoded.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyID.Key)
	}
}
