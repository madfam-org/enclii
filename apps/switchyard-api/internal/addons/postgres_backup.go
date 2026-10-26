package addons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Backups for managed-Postgres addons, provisioned AND converged.
//
// WHY THIS FILE EXISTS (2026-10-26). `AddonBackupNeverCompleted` fired for
// every live addon cluster. None of them had ever been backed up, because four
// independent links were missing, and any one of them was enough:
//
//  1. switchyard-api ran without ENCLII_ADDON_BACKUP_DESTINATION_BASE, so the
//     provisioner left `spec.backup` out (and said so at ERROR, on every
//     provision, in a log nobody reads);
//  2. the credential Secret it replicates (enclii/enclii-db-backup-credentials)
//     had never been created;
//  3. the switchyard-api ClusterRole could not touch `scheduledbackups`, so the
//     ScheduledBackup create would have been refused anyway;
//  4. the addon namespace's default-deny admits DNS egress only, and the
//     control-plane policy opens 443 to 10.0.0.0/8 only, so an instance could
//     not have reached the object store even with all of the above.
//
// And nothing would have healed a cluster that existed before the links were
// fixed: backup wiring ran only inside Provision, which never runs again.
//
// So the wiring is now a CONVERGENCE, run by Provision for a new cluster and by
// the addon reconciler's backup sweep for every live one, in an order chosen so
// that no step can leave a database worse off than it was:
//
//	credential copy  →  egress policy  →  Cluster spec.backup  →  ScheduledBackup
//
// Setting spec.backup turns on WAL archiving to the object store. If the
// instance could not then authenticate to, or reach, the store, archive_command
// would fail, WAL would pile up in pg_wal and the volume would eventually fill.
// That is why the credential and the egress rule land first, and why the whole
// convergence refuses (ErrBackupCredentialsMissing) instead of half-wiring a
// cluster while the credential is absent.
//
// What is never done here: overwrite a backup stanza that differs from the one
// Enclii would write (ErrBackupStanzaDrift: a human chose it, a human changes
// it), wire a hibernated cluster (its instances are stopped; it converges when
// it wakes), or touch a cluster Enclii does not manage.
//
// The one write a hibernated cluster gets: its Enclii ScheduledBackup, if it
// has one, is SUSPENDED (and marked as suspended for hibernation), because a
// schedule on a cluster with no instance produces a failed Backup every night.
// When the cluster wakes, the sweep resumes only a schedule it suspended.

var (
	// ErrBackupConvergenceNotApplicable: nothing to converge. Not a live
	// managed-Postgres addon, a staging profile that is backup-free by design,
	// a cluster Enclii does not manage, a cluster that does not exist yet, or a
	// hibernated one. The sweep counts these as skipped, not failed.
	ErrBackupConvergenceNotApplicable = errors.New("backup convergence does not apply to this addon")
	// ErrBackupStoreNotConfigured: ENCLII_ADDON_BACKUP_DESTINATION_BASE is unset.
	ErrBackupStoreNotConfigured = errors.New("addon backup store is not configured: ENCLII_ADDON_BACKUP_DESTINATION_BASE is unset")
	// ErrBackupCredentialsMissing: the source credential Secret is absent or
	// lacks a key. Nothing was written.
	ErrBackupCredentialsMissing = errors.New("addon backup credential Secret " + BackupCredentialsSourceNS + "/" + BackupCredentialsSecretName + " is missing or incomplete; nothing was written")
	// ErrBackupStanzaDrift: the Cluster already carries a backup stanza that is
	// not the one Enclii writes. It is reported, never overwritten.
	ErrBackupStanzaDrift = errors.New("cluster carries a backup stanza Enclii did not write; not overwritten")
)

// backupCredentialKeys are the keys CNPG's s3Credentials reference.
var backupCredentialKeys = []string{"ACCESS_KEY_ID", "SECRET_ACCESS_KEY"}

// backupFieldManager names Enclii's writes to the Cluster and ScheduledBackup.
const backupFieldManager = "enclii-addon-backups"

// suspendedForHibernation marks a ScheduledBackup Enclii suspended because its
// cluster hibernated, so waking resumes it and never a schedule an operator
// suspended on purpose.
const suspendedForHibernation = "enclii.dev/suspended-for-hibernation"

// BackupStore is the configured addon backup object store, as the provisioner
// and the backup sweep see it. Exported for ops.addons.backup-credential,
// which provisions the store's credential and must target exactly the bucket
// and endpoint the clusters will use.
type BackupStore struct {
	DestinationBase string
	EndpointURL     string
}

// BackupStoreFromEnv reads ENCLII_ADDON_BACKUP_DESTINATION_BASE and
// ENCLII_ADDON_BACKUP_ENDPOINT_URL.
func BackupStoreFromEnv() BackupStore {
	return BackupStore(backupConfigFromEnv())
}

// Bucket returns the bucket of an s3://<bucket>[/<prefix>] destination base,
// or an error when the base is unset or not of that form.
func (b BackupStore) Bucket() (string, error) {
	base := strings.TrimSpace(b.DestinationBase)
	if base == "" {
		return "", ErrBackupStoreNotConfigured
	}
	rest, ok := strings.CutPrefix(base, "s3://")
	if !ok {
		return "", fmt.Errorf("ENCLII_ADDON_BACKUP_DESTINATION_BASE %q is not an s3:// URL", base)
	}
	bucket, _, _ := strings.Cut(rest, "/")
	if bucket == "" {
		return "", fmt.Errorf("ENCLII_ADDON_BACKUP_DESTINATION_BASE %q names no bucket", base)
	}
	return bucket, nil
}

// backupStanza is the spec.backup Enclii writes for one cluster: barman object
// store at a per-cluster prefix, the replicated credential, and retention.
// CNPG archives WAL to the same destination, so this is base backups AND
// continuous WAL archiving.
func (b backupConfig) backupStanza(namespace, resourceName string) map[string]interface{} {
	store := map[string]interface{}{
		"destinationPath": b.destinationPath(namespace, resourceName),
		"s3Credentials": map[string]interface{}{
			"accessKeyId": map[string]interface{}{
				"name": BackupCredentialsSecretName,
				"key":  backupCredentialKeys[0],
			},
			"secretAccessKey": map[string]interface{}{
				"name": BackupCredentialsSecretName,
				"key":  backupCredentialKeys[1],
			},
		},
	}
	if b.EndpointURL != "" {
		store["endpointURL"] = b.EndpointURL
	}
	return map[string]interface{}{
		"retentionPolicy":   BackupRetention,
		"barmanObjectStore": store,
	}
}

func (b backupConfig) destinationPath(namespace, resourceName string) string {
	return fmt.Sprintf("%s/%s/%s", strings.TrimRight(b.DestinationBase, "/"), namespace, resourceName)
}

// backupStanzaMatches compares only the fields Enclii writes. CNPG's webhook
// defaults others into spec.backup (target, for one), so a whole-object
// comparison would report drift on every cluster it ever touched.
func backupStanzaMatches(existing map[string]interface{}, desired map[string]interface{}) bool {
	str := func(obj map[string]interface{}, path ...string) string {
		v, _, _ := unstructured.NestedString(obj, path...)
		return v
	}
	for _, path := range [][]string{
		{"retentionPolicy"},
		{"barmanObjectStore", "destinationPath"},
		{"barmanObjectStore", "endpointURL"},
		{"barmanObjectStore", "s3Credentials", "accessKeyId", "name"},
		{"barmanObjectStore", "s3Credentials", "accessKeyId", "key"},
		{"barmanObjectStore", "s3Credentials", "secretAccessKey", "name"},
		{"barmanObjectStore", "s3Credentials", "secretAccessKey", "key"},
	} {
		if str(existing, path...) != str(desired, path...) {
			return false
		}
	}
	return true
}

// buildBackupEgressPolicy lets one CNPG cluster's pods reach the object store:
// TCP 443 to public addresses. The addon namespace's default-deny admits DNS
// only and the control-plane policy opens 443 to 10.0.0.0/8 only, so without
// this rule barman-cloud cannot connect and WAL archiving fails.
//
// Trade-off, stated: the object store is Cloudflare R2, served from anycast
// ranges with no stable narrow CIDR, and NetworkPolicy has no FQDN peer. So
// the rule opens HTTPS to every non-private address for this cluster's pods,
// the same shape service namespaces already get (reconciler/namespace.go). It
// is written only for a cluster whose backups are being wired.
func buildBackupEgressPolicy(namespace, resourceName string) *networkingv1.NetworkPolicy {
	https := intstr.FromInt(443)
	tcp := corev1.ProtocolTCP
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName + "-backup-egress",
			Namespace: namespace,
			Labels:    map[string]string{"enclii.dev/managed-by": "onboarding-api"},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{cnpgClusterLabel: resourceName}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{
					CIDR:   "0.0.0.0/0",
					Except: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
				}}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &https}},
			}},
		},
	}
}

// backupCredentialsReady checks that the source credential Secret exists and
// carries both keys with a value. It reads key NAMES and lengths only; no
// value is returned, compared or logged.
func (p *PostgresProvisioner) backupCredentialsReady(ctx context.Context) error {
	if p == nil || p.k8sClient == nil {
		return errors.New("postgres provisioner has no cluster client")
	}
	secret, err := p.k8sClient.Kube().CoreV1().Secrets(BackupCredentialsSourceNS).Get(ctx, BackupCredentialsSecretName, metav1.GetOptions{})
	if err != nil {
		if isNotFoundError(err) {
			return ErrBackupCredentialsMissing
		}
		return fmt.Errorf("read backup credential Secret %s/%s: %w", BackupCredentialsSourceNS, BackupCredentialsSecretName, err)
	}
	for _, key := range backupCredentialKeys {
		if len(secret.Data[key]) == 0 {
			return fmt.Errorf("%w (key %s absent or empty)", ErrBackupCredentialsMissing, key)
		}
	}
	return nil
}

// ensureScheduledBackup creates the cluster's daily ScheduledBackup when it is
// absent. An existing one is left as it is.
func (p *PostgresProvisioner) ensureScheduledBackup(ctx context.Context, req *ProvisionRequest, resourceName string) (bool, error) {
	client := p.dynamicClient.Resource(cnpgScheduledBackupGVR).Namespace(req.Namespace)
	if _, err := client.Get(ctx, resourceName+"-daily", metav1.GetOptions{}); err == nil {
		return false, nil
	} else if !isNotFoundError(err) {
		return false, fmt.Errorf("read ScheduledBackup %s/%s-daily: %w", req.Namespace, resourceName, err)
	}
	raw, err := json.Marshal(p.buildScheduledBackupManifest(req, resourceName))
	if err != nil {
		return false, fmt.Errorf("marshal ScheduledBackup: %w", err)
	}
	var obj unstructured.Unstructured
	if err := json.Unmarshal(raw, &obj.Object); err != nil {
		return false, fmt.Errorf("convert ScheduledBackup: %w", err)
	}
	if _, err := client.Create(ctx, &obj, metav1.CreateOptions{FieldManager: backupFieldManager}); err != nil {
		return false, fmt.Errorf("create ScheduledBackup %s/%s-daily: %w", req.Namespace, resourceName, err)
	}
	return true, nil
}

// BackupConvergence reports what one convergence changed.
type BackupConvergence struct {
	StanzaPatched          bool `json:"stanza_patched"`
	ScheduledBackupCreated bool `json:"scheduled_backup_created"`
	// ScheduleSuspended / ScheduleResumed: the cluster hibernated / woke and
	// its Enclii schedule followed.
	ScheduleSuspended bool `json:"schedule_suspended,omitempty"`
	ScheduleResumed   bool `json:"schedule_resumed,omitempty"`
}

// setScheduleSuspended suspends (hibernated) or resumes (awake) the cluster's
// Enclii ScheduledBackup. It resumes only a schedule it suspended itself, and
// reports whether it wrote. A cluster with no schedule is left alone.
func (p *PostgresProvisioner) setScheduleSuspended(ctx context.Context, namespace, resourceName string, suspend bool) (bool, error) {
	client := p.dynamicClient.Resource(cnpgScheduledBackupGVR).Namespace(namespace)
	sb, err := client.Get(ctx, resourceName+"-daily", metav1.GetOptions{})
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("read ScheduledBackup %s/%s-daily: %w", namespace, resourceName, err)
	}
	suspended, _, _ := unstructured.NestedBool(sb.Object, "spec", "suspend")
	ours := sb.GetAnnotations()[suspendedForHibernation] == "true"
	var patch map[string]interface{}
	switch {
	case suspend && !suspended:
		patch = map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": map[string]interface{}{suspendedForHibernation: "true"}},
			"spec":     map[string]interface{}{"suspend": true},
		}
	case !suspend && suspended && ours:
		patch = map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": map[string]interface{}{suspendedForHibernation: nil}},
			"spec":     map[string]interface{}{"suspend": false},
		}
	default:
		return false, nil
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return false, fmt.Errorf("marshal ScheduledBackup patch: %w", err)
	}
	if _, err := client.Patch(ctx, resourceName+"-daily", k8stypes.MergePatchType, raw, metav1.PatchOptions{FieldManager: backupFieldManager}); err != nil {
		return false, fmt.Errorf("set suspend=%t on ScheduledBackup %s/%s-daily: %w", suspend, namespace, resourceName, err)
	}
	return true, nil
}

// convergeBackups wires backups onto one existing cluster, in the order the
// file comment explains. Idempotent: a converged cluster costs four reads.
func (p *PostgresProvisioner) convergeBackups(ctx context.Context, addon *types.DatabaseAddon) (BackupConvergence, error) {
	var out BackupConvergence
	cfg := backupConfigFromEnv()
	if !cfg.enabled() {
		return out, ErrBackupStoreNotConfigured
	}
	if p == nil || p.k8sClient == nil || p.dynamicClient == nil {
		return out, errors.New("postgres provisioner has no cluster client")
	}
	ns, name := addon.K8sNamespace, addon.K8sResourceName
	cluster, err := p.dynamicClient.Resource(cnpgGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if isNotFoundError(err) {
			return out, fmt.Errorf("%w: Cluster %s/%s does not exist", ErrBackupConvergenceNotApplicable, ns, name)
		}
		return out, fmt.Errorf("read CNPG cluster %s/%s: %w", ns, name, err)
	}
	labels := cluster.GetLabels()
	if labels[LabelManagedBy] != LabelManagedValue || labels[LabelAddonID] != addon.ID.String() {
		return out, fmt.Errorf("%w: Cluster %s/%s is not managed by Enclii for this addon", ErrBackupConvergenceNotApplicable, ns, name)
	}
	if cluster.GetAnnotations()[CNPGHibernationAnnotation] == "on" {
		suspended, err := p.setScheduleSuspended(ctx, ns, name, true)
		if err != nil {
			return out, err
		}
		out.ScheduleSuspended = suspended
		return out, fmt.Errorf("%w: Cluster %s/%s is hibernated", ErrBackupConvergenceNotApplicable, ns, name)
	}
	desired := cfg.backupStanza(ns, name)
	existing, hasStanza, _ := unstructured.NestedMap(cluster.Object, "spec", "backup")
	if hasStanza && !backupStanzaMatches(existing, desired) {
		return out, fmt.Errorf("%w: Cluster %s/%s", ErrBackupStanzaDrift, ns, name)
	}
	if err := p.backupCredentialsReady(ctx); err != nil {
		return out, err
	}
	owner := k8s.SecretCopyOwner{APIVersion: CloudNativePGAPIVersion, Kind: CloudNativePGKind, Name: name, UID: cluster.GetUID()}
	if err := p.k8sClient.EnsureSecretCopiedFrom(ctx, BackupCredentialsSourceNS, BackupCredentialsSecretName, ns, owner); err != nil {
		return out, fmt.Errorf("replicate backup credential into %s (Cluster left unchanged): %w", ns, err)
	}
	if err := p.k8sClient.EnsureNetworkPolicy(ctx, buildBackupEgressPolicy(ns, name)); err != nil {
		return out, fmt.Errorf("backup egress policy for %s/%s (Cluster left unchanged): %w", ns, name, err)
	}
	if !hasStanza {
		patch, err := json.Marshal(map[string]interface{}{"spec": map[string]interface{}{"backup": desired}})
		if err != nil {
			return out, fmt.Errorf("marshal backup patch: %w", err)
		}
		if _, err := p.dynamicClient.Resource(cnpgGVR).Namespace(ns).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{FieldManager: backupFieldManager}); err != nil {
			return out, fmt.Errorf("set spec.backup on Cluster %s/%s: %w", ns, name, err)
		}
		out.StanzaPatched = true
	}
	req := &ProvisionRequest{Addon: addon, Namespace: ns, ProjectID: addon.ProjectID}
	created, err := p.ensureScheduledBackup(ctx, req, name)
	if err != nil {
		return out, err
	}
	out.ScheduledBackupCreated = created
	if !created {
		resumed, err := p.setScheduleSuspended(ctx, ns, name, false)
		if err != nil {
			return out, err
		}
		out.ScheduleResumed = resumed
	}
	return out, nil
}

// prepareProvisionBackups decides, before a new Cluster is created, whether its
// manifest may carry spec.backup. It may only when the store is configured AND
// the credential exists: a stanza without a credential would start a database
// whose WAL archiving fails from its first segment. Every refusal is logged at
// ERROR and left for the reconciler's backup sweep to converge.
func (p *PostgresProvisioner) prepareProvisionBackups(ctx context.Context, cluster map[string]interface{}, logger *logrus.Entry) bool {
	if !backupConfigFromEnv().enabled() {
		logger.Error("ENCLII_ADDON_BACKUP_DESTINATION_BASE is not set — this cluster is being provisioned WITHOUT backups")
		return false
	}
	if err := p.backupCredentialsReady(ctx); err != nil {
		if spec, ok := cluster["spec"].(map[string]interface{}); ok {
			delete(spec, "backup")
		}
		logger.WithError(err).Error("Backup store configured but its credential is not ready — provisioning WITHOUT backups; the reconciler's backup sweep wires them once the credential exists")
		return false
	}
	return true
}

// wireNewClusterBackups is Provision's half, right after the Cluster exists
// (its UID owns the credential copy): credential, egress rule, schedule. The
// stanza is already in the manifest. Each failure is logged at ERROR and left
// for the backup sweep, which converges the same three objects.
func (p *PostgresProvisioner) wireNewClusterBackups(ctx context.Context, req *ProvisionRequest, resourceName string, owner k8s.SecretCopyOwner, logger *logrus.Entry) {
	if err := p.k8sClient.EnsureSecretCopiedFrom(ctx, BackupCredentialsSourceNS, BackupCredentialsSecretName, req.Namespace, owner); err != nil {
		logger.WithError(err).Error("Failed to replicate backup credentials — barman will not be able to write backups")
	}
	if err := p.k8sClient.EnsureNetworkPolicy(ctx, buildBackupEgressPolicy(req.Namespace, resourceName)); err != nil {
		logger.WithError(err).Error("Failed to create backup egress NetworkPolicy — barman cannot reach the object store")
	}
	if _, err := p.ensureScheduledBackup(ctx, req, resourceName); err != nil {
		logger.WithError(err).Error("Failed to create ScheduledBackup — cluster has no recurring backups")
	}
}

// ConvergeAddonBackups wires backups onto one live managed-Postgres addon
// cluster (see the file comment). Called by the addon reconciler's backup
// sweep for every live cluster.
func (s *AddonService) ConvergeAddonBackups(ctx context.Context, addon *types.DatabaseAddon) (BackupConvergence, error) {
	if isKarafielStagingAddon(addon) || !networkConvergible(addon) {
		return BackupConvergence{}, ErrBackupConvergenceNotApplicable
	}
	pg, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok || pg == nil {
		return BackupConvergence{}, errors.New("postgres provisioner is not configured")
	}
	return pg.convergeBackups(ctx, addon)
}
