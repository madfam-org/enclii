package addons

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// The two 2026-08-17 findings, pinned so they cannot regress:
//
//  1. Version pinning was DEAD CODE — the manifest carried a
//     `postgresVersion` field CNPG does not define, the API server pruned it
//     silently, and clusters "pinned" to 16 ran 18.3. The image tag is the
//     only real pin.
//  2. NO addon cluster had backups — 28 provisioned, last_backup_at NEVER on
//     every one. The backup stanza and the ScheduledBackup are the fix, and
//     their absence when unconfigured must be a deliberate, testable state.

func provisionReq(version string) *ProvisionRequest {
	return &ProvisionRequest{
		Addon: &types.DatabaseAddon{
			ID:   uuid.New(),
			Name: "map",
			Config: types.DatabaseAddonConfig{
				Version: version,
			},
		},
		Namespace: "project-crea",
		ProjectID: uuid.New(),
	}
}

func TestManifestPinsVersionViaImageNameNotPrunedField(t *testing.T) {
	p := &PostgresProvisioner{}
	cluster := p.buildClusterManifest(provisionReq("17"), "pg-map-abc12345")
	spec := cluster["spec"].(map[string]interface{})

	if _, dead := spec["postgresVersion"]; dead {
		t.Fatal("postgresVersion is not a CNPG field; the API server prunes it — the pin must be imageName")
	}
	if got := spec["imageName"]; got != "ghcr.io/cloudnative-pg/postgresql:17" {
		t.Fatalf("imageName = %v; want the requested major as the image tag", got)
	}
}

func TestManifestDefaultsToHonestCurrentMajor(t *testing.T) {
	p := &PostgresProvisioner{}
	cluster := p.buildClusterManifest(provisionReq(""), "pg-map-abc12345")
	spec := cluster["spec"].(map[string]interface{})
	want := fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%d", DefaultPostgresVersion)
	if got := spec["imageName"]; got != want {
		t.Fatalf("imageName = %v; want default %s", got, want)
	}
}

func TestManifestCarriesBackupStanzaWhenConfigured(t *testing.T) {
	t.Setenv("ENCLII_ADDON_BACKUP_DESTINATION_BASE", "s3://enclii-db-backups/")
	t.Setenv("ENCLII_ADDON_BACKUP_ENDPOINT_URL", "https://accountid.r2.cloudflarestorage.com")

	p := &PostgresProvisioner{}
	cluster := p.buildClusterManifest(provisionReq(""), "pg-map-abc12345")
	spec := cluster["spec"].(map[string]interface{})

	backup, ok := spec["backup"].(map[string]interface{})
	if !ok {
		t.Fatal("configured store must produce a spec.backup stanza")
	}
	if backup["retentionPolicy"] != BackupRetention {
		t.Fatalf("retentionPolicy = %v", backup["retentionPolicy"])
	}
	barman := backup["barmanObjectStore"].(map[string]interface{})
	// Per-cluster path, trailing slash on the base normalized away.
	if got := barman["destinationPath"]; got != "s3://enclii-db-backups/project-crea/pg-map-abc12345" {
		t.Fatalf("destinationPath = %v", got)
	}
	creds := barman["s3Credentials"].(map[string]interface{})
	access := creds["accessKeyId"].(map[string]interface{})
	if access["name"] != BackupCredentialsSecretName {
		t.Fatalf("credentials secret = %v; must be the replicated %s", access["name"], BackupCredentialsSecretName)
	}
}

func TestManifestOmitsBackupWhenUnconfigured(t *testing.T) {
	t.Setenv("ENCLII_ADDON_BACKUP_DESTINATION_BASE", "")

	p := &PostgresProvisioner{}
	cluster := p.buildClusterManifest(provisionReq(""), "pg-map-abc12345")
	spec := cluster["spec"].(map[string]interface{})
	if _, has := spec["backup"]; has {
		t.Fatal("unconfigured store must omit the stanza (Provision logs the gap loudly instead)")
	}
}

func TestScheduledBackupPairsWithCluster(t *testing.T) {
	p := &PostgresProvisioner{}
	req := provisionReq("")
	sb := p.buildScheduledBackupManifest(req, "pg-map-abc12345")

	if sb["kind"] != "ScheduledBackup" {
		t.Fatalf("kind = %v", sb["kind"])
	}
	meta := sb["metadata"].(map[string]interface{})
	if meta["name"] != "pg-map-abc12345-daily" {
		t.Fatalf("name = %v; Deprovision deletes by this convention", meta["name"])
	}
	spec := sb["spec"].(map[string]interface{})
	// A restore point that does not exist yet protects nothing.
	if spec["immediate"] != true {
		t.Fatal("first backup must run immediately, not tomorrow")
	}
	if spec["schedule"] != BackupSchedule {
		t.Fatalf("schedule = %v", spec["schedule"])
	}
	cluster := spec["cluster"].(map[string]interface{})
	if cluster["name"] != "pg-map-abc12345" {
		t.Fatalf("cluster ref = %v", cluster["name"])
	}
}
