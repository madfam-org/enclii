package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const initialJobManager = "isolated-initial-bootstrap"

// Fixed metadata-only proof before Prisma DDL. A second request key cannot
// turn an initial profile into migration/reset of an already populated DB.
const initialMAPFreshGuard = `let c;
(async()=>{try{
 const {PrismaClient}=require('/app/node_modules/@prisma/client');c=new PrismaClient();
 const u=new URL(process.env.DATABASE_URL);
 if(u.username!=='enclii_migrator'||!/^\/[a-z][a-z0-9_]*_sim$/.test(u.pathname))throw Error();
 const a=await c.$queryRawUnsafe("SELECT current_database() AS base,current_user AS cuenta,r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication AS privilegiada,d.datdba=r.oid AS propietaria,EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid) AS membresias,(SELECT count(*) FROM pg_tables WHERE schemaname='public') AS tablas FROM pg_roles r JOIN pg_database d ON d.datname=current_database() WHERE r.rolname=current_user");
 if(a.length!==1||a[0].cuenta!=='enclii_migrator'||a[0].base!==u.pathname.slice(1)||a[0].privilegiada||!a[0].propietaria||a[0].membresias||a[0].tablas!==0n)throw Error();
 process.stdout.write('INITIAL_DATABASE_EMPTY\n');
}catch{process.stderr.write('Initial database guard refused.\n');process.exitCode=1;}finally{if(c)await c.$disconnect().catch(()=>{});}})();`

// A server-built plan: HTTP requests never bind this type. Data is kept out of
// JSON and is transported only to a short-lived Secret owned by the fixed Job.
type IsolatedInitialJobPlan struct {
	ProjectID, AddonID, ServiceID                              uuid.UUID
	Namespace, Cluster, Profile, Image, IdempotencyKey, Reason string
	Data                                                       map[string][]byte           `json:"-"`
	Revalidate                                                 func(context.Context) error `json:"-"`
}

type IsolatedInitialJobReceipt struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	DryRun    bool   `json:"dry_run"`
	Suspended bool   `json:"suspended"`
	Succeeded int32  `json:"succeeded"`
	Failed    int32  `json:"failed"`
	Active    int32  `json:"active"`
}

func (p IsolatedInitialJobPlan) jobName() string {
	sum := sha256.Sum256([]byte(p.ProjectID.String() + "/" + p.AddonID.String() + "/" + p.ServiceID.String() + "/" + p.Profile + "/" + p.IdempotencyKey))
	return "initial-" + hex.EncodeToString(sum[:12])
}

func (p IsolatedInitialJobPlan) jobLabels() map[string]string {
	return map[string]string{"enclii.dev/managed-by": initialJobManager, LabelProjectNamespace: p.ProjectID.String(), "enclii.dev/addon-id": p.AddonID.String(), "enclii.dev/service-id": p.ServiceID.String(), "enclii.dev/bootstrap-profile": p.Profile, "enclii.dev/bootstrap-job": p.jobName()}
}

func (p IsolatedInitialJobPlan) fingerprint() string {
	// Credential values never enter annotations or responses, only this
	// nonreversible fingerprint for replay/rotation consistency.
	body, _ := json.Marshal(struct {
		Image, Profile string
		Data           map[string][]byte
	}{p.Image, p.Profile, p.Data})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func BuildIsolatedInitialJob(p IsolatedInitialJobPlan) (*batchv1.Job, error) {
	if p.ProjectID == uuid.Nil || p.AddonID == uuid.Nil || p.ServiceID == uuid.Nil || p.Namespace != "project-"+p.ProjectID.String()[:8] || p.Cluster == "" || p.IdempotencyKey == "" || len(p.IdempotencyKey) > 128 || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 512 || p.Data["ca.crt"] == nil {
		return nil, fmt.Errorf("incomplete isolated initial Job plan")
	}
	digestParts := strings.Split(p.Image, "@sha256:")
	if len(digestParts) != 2 || len(digestParts[1]) != 64 || strings.ToLower(digestParts[1]) != digestParts[1] {
		return nil, fmt.Errorf("initial image requires a canonical digest")
	}
	if _, err := hex.DecodeString(digestParts[1]); err != nil {
		return nil, fmt.Errorf("initial image digest malformed")
	}
	var command, migration []string
	allowed := map[string]bool{"ca.crt": true}
	switch p.Profile {
	case "map-sim-initial":
		if !strings.HasPrefix(p.Image, "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:") {
			return nil, fmt.Errorf("initial MAP producer mismatch")
		}
		command = []string{"node", "/app/dist/semilla-simulacion.cjs", "--aplicar"}
		migration = []string{"node", "/app/prisma-cli/node_modules/prisma/build/index.js", "migrate", "deploy", "--schema=/app/prisma/schema.prisma"}
		for _, name := range []string{"DATABASE_URL", "MAP_SIMULATION_RUNTIME_DATABASE_URL", "MAP_SIMULATION_SEED_ENABLED", "MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"} {
			allowed[name] = true
		}
	case "portal-sim-initial":
		if !strings.HasPrefix(p.Image, "ghcr.io/madfam-org/crea-sim/gateway@sha256:") {
			return nil, fmt.Errorf("initial portal producer mismatch")
		}
		command = []string{"node", "packages/entrenamiento/src/migrar.ts"}
		allowed["CREA_SIM_MIGRATOR_DATABASE_URL"], allowed["CREA_SIM_DATABASE_NAMESPACE"] = true, true
	default:
		return nil, fmt.Errorf("unsupported initial Job profile")
	}
	if len(p.Data) != len(allowed) {
		return nil, fmt.Errorf("initial Job environment is incomplete or broadened")
	}
	var keys []string
	for key, value := range p.Data {
		if !allowed[key] || len(value) == 0 {
			return nil, fmt.Errorf("initial Job environment refused")
		}
		if key != "ca.crt" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	environment := []corev1.EnvVar{{Name: "NODE_EXTRA_CA_CERTS", Value: "/var/run/crea-sim/ca/ca.crt"}}
	for _, key := range keys {
		environment = append(environment, corev1.EnvVar{Name: key, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: p.jobName() + "-credentials"}, Key: key}}})
	}
	container := corev1.Container{Name: "initial", Image: p.Image, ImagePullPolicy: corev1.PullAlways, Command: command, Env: environment,
		SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
		Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("1Gi")}},
		VolumeMounts:    []corev1.VolumeMount{{Name: "ca", MountPath: "/var/run/crea-sim/ca", ReadOnly: true}, {Name: "tmp", MountPath: "/tmp"}}}
	job := &batchv1.Job{TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"}, ObjectMeta: metav1.ObjectMeta{Name: p.jobName(), Namespace: p.Namespace, Labels: p.jobLabels(), Annotations: map[string]string{"enclii.dev/bootstrap-fingerprint": p.fingerprint(), "enclii.dev/operation-reason": p.Reason}},
		Spec: batchv1.JobSpec{Suspend: ptr.To(true), BackoffLimit: ptr.To[int32](0), ActiveDeadlineSeconds: ptr.To[int64](600), TTLSecondsAfterFinished: ptr.To[int32](3600), Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: p.jobLabels()}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: ptr.To(false), ImagePullSecrets: []corev1.LocalObjectReference{{Name: "ghcr-credentials"}}, SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), FSGroup: ptr.To[int64](1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{container}, Volumes: []corev1.Volume{{Name: "ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: p.jobName() + "-credentials", Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}}}, {Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}}}}}
	if len(migration) > 0 {
		job.Spec.Template.Spec.SecurityContext.RunAsUser = ptr.To[int64](1001)
		job.Spec.Template.Spec.SecurityContext.RunAsGroup = ptr.To[int64](1001)
		job.Spec.Template.Spec.SecurityContext.FSGroup = ptr.To[int64](1001)
		fresh := container.DeepCopy()
		fresh.Name = "fresh-only"
		fresh.Command = []string{"node", "-e", initialMAPFreshGuard}
		init := container.DeepCopy()
		init.Name = "migrate"
		init.Command = migration
		job.Spec.Template.Spec.InitContainers = []corev1.Container{*fresh, *init}
	}
	return job, nil
}

func initialJobReceipt(job *batchv1.Job, dryRun bool) IsolatedInitialJobReceipt {
	return IsolatedInitialJobReceipt{Name: job.Name, Namespace: job.Namespace, DryRun: dryRun, Suspended: job.Spec.Suspend != nil && *job.Spec.Suspend, Succeeded: job.Status.Succeeded, Failed: job.Status.Failed, Active: job.Status.Active}
}

func (c *Client) checkInitialJobNamespace(ctx context.Context, p IsolatedInitialJobPlan) error {
	if c == nil || c.kubeClient() == nil || c.DynamicClient == nil {
		return fmt.Errorf("initial Job adapter unavailable")
	}
	ns, err := c.kubeClient().CoreV1().Namespaces().Get(ctx, p.Namespace, metav1.GetOptions{})
	if err != nil || ns.UID == "" || ns.Labels[LabelProjectNamespace] != p.ProjectID.String() || ns.Labels["enclii.dev/verify-signatures"] != "true" {
		return ErrNamespaceOwnership
	}
	policy, err := c.DynamicClient.Resource(schema.GroupVersionResource{Group: "kyverno.io", Version: "v1", Resource: "clusterpolicies"}).Get(ctx, "verify-image-signatures", metav1.GetOptions{})
	if err != nil || policy.Object["spec"] == nil {
		return fmt.Errorf("actual signature policy unavailable")
	}
	spec, ok := policy.Object["spec"].(map[string]interface{})
	if !ok || spec["validationFailureAction"] != "Enforce" {
		return fmt.Errorf("actual signature policy is not enforcing")
	}
	rules, found, parseErr := unstructured.NestedSlice(policy.Object, "spec", "rules")
	if parseErr != nil || !found {
		return fmt.Errorf("actual signature rule unavailable")
	}
	required := false
	for _, raw := range rules {
		rule, valid := raw.(map[string]interface{})
		if !valid || rule["name"] != "check-signature" {
			continue
		}
		if rule["exclude"] != nil {
			continue
		}
		matched := false
		matches, _, _ := unstructured.NestedSlice(rule, "match", "any")
		for _, rawMatch := range matches {
			match, valid := rawMatch.(map[string]interface{})
			if !valid {
				continue
			}
			kinds, _, _ := unstructured.NestedStringSlice(match, "resources", "kinds")
			podKind := false
			for _, kind := range kinds {
				if kind == "Pod" {
					podKind = true
				}
			}
			selectorMap, found, _ := unstructured.NestedMap(match, "resources", "namespaceSelector")
			if !podKind || !found {
				continue
			}
			encoded, _ := json.Marshal(selectorMap)
			var selector metav1.LabelSelector
			if json.Unmarshal(encoded, &selector) != nil {
				continue
			}
			nsSelector, selectorErr := metav1.LabelSelectorAsSelector(&selector)
			if selectorErr == nil && nsSelector.Matches(labels.Set(ns.Labels)) {
				matched = true
			}
		}
		if !matched {
			continue
		}
		images, _, _ := unstructured.NestedSlice(rule, "verifyImages")
		for _, rawImage := range images {
			image, valid := rawImage.(map[string]interface{})
			if !valid || image["required"] != true || image["verifyDigest"] != true {
				continue
			}
			refs, _, _ := unstructured.NestedStringSlice(image, "imageReferences")
			for _, ref := range refs {
				if ref == "ghcr.io/madfam-org/*" {
					required = true
				}
			}
		}
	}
	if !required {
		return fmt.Errorf("actual signature rule is not required for the producer")
	}
	policies, err := c.kubeClient().NetworkingV1().NetworkPolicies(p.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("initial Job network state unavailable")
	}
	defaultDeny := false
	for _, rule := range policies.Items {
		selector, parseErr := metav1.LabelSelectorAsSelector(&rule.Spec.PodSelector)
		if parseErr != nil {
			return fmt.Errorf("initial Job network selector malformed")
		}
		if !selector.Matches(labels.Set(p.jobLabels())) {
			continue
		}
		if rule.Name == "default-deny" && reflect.DeepEqual(rule.Spec.PodSelector, metav1.LabelSelector{}) && len(rule.Spec.Ingress) == 0 && reflect.DeepEqual(rule.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}) {
			defaultDeny = true
		}
		if rule.Name == p.jobName()+"-database" {
			job, getErr := c.kubeClient().BatchV1().Jobs(p.Namespace).Get(ctx, p.jobName(), metav1.GetOptions{})
			expected := initialDatabasePolicy(p, metav1.OwnerReference{})
			if getErr != nil || job.UID == "" || rule.Labels["enclii.dev/managed-by"] != initialJobManager || rule.Labels[LabelProjectNamespace] != p.ProjectID.String() || !reflect.DeepEqual(rule.Spec, expected.Spec) || len(rule.OwnerReferences) != 1 || rule.OwnerReferences[0].UID != job.UID || rule.OwnerReferences[0].Kind != "Job" || rule.OwnerReferences[0].Name != job.Name {
				return fmt.Errorf("initial database egress ownership conflicts")
			}
			continue
		}
		for _, egress := range rule.Spec.Egress {
			if len(egress.Ports) == 0 {
				return fmt.Errorf("initial Job has broad egress")
			}
			for _, port := range egress.Ports {
				if port.Port == nil || port.Port.Type != intstr.Int || port.Port.IntVal != 53 {
					return fmt.Errorf("initial Job has non-DNS foreign egress")
				}
			}
		}
	}
	if !defaultDeny {
		return fmt.Errorf("initial Job default-deny missing")
	}
	return nil
}

func (c *Client) admitInitialJob(ctx context.Context, job *batchv1.Job, includeJob bool) error {
	options := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	if includeJob {
		if _, err := c.kubeClient().BatchV1().Jobs(job.Namespace).Create(ctx, job, options); err != nil {
			return fmt.Errorf("actual initial Job admission refused")
		}
	}
	pod := &corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-admission", Namespace: job.Namespace, Labels: job.Spec.Template.Labels}, Spec: job.Spec.Template.Spec}
	if _, err := c.kubeClient().CoreV1().Pods(job.Namespace).Create(ctx, pod, options); err != nil {
		return fmt.Errorf("actual initial Pod admission refused")
	}
	return nil
}

// Create suspended first, prepare owned transient custody/network, recheck
// authority, then activate once. A timeout never starts an automatic retry.
func (c *Client) RunIsolatedInitialJob(ctx context.Context, p IsolatedInitialJobPlan, dryRun bool) (*IsolatedInitialJobReceipt, error) {
	job, err := BuildIsolatedInitialJob(p)
	if err != nil {
		return nil, err
	}
	if err = c.checkInitialJobNamespace(ctx, p); err != nil {
		return nil, err
	}
	jobs := c.kubeClient().BatchV1().Jobs(p.Namespace)
	existing, err := jobs.Get(ctx, job.Name, metav1.GetOptions{})
	fresh := k8serrors.IsNotFound(err)
	if err == nil {
		if existing.UID == "" || !reflect.DeepEqual(existing.Labels, job.Labels) || existing.Annotations["enclii.dev/bootstrap-fingerprint"] != p.fingerprint() {
			return nil, fmt.Errorf("initial Job idempotency conflicts with existing object")
		}
		if existing.Spec.Suspend == nil || !*existing.Spec.Suspend || existing.Status.Active > 0 || existing.Status.Succeeded > 0 || existing.Status.Failed > 0 {
			receipt := initialJobReceipt(existing, dryRun)
			return &receipt, nil
		}
		job = existing
	} else if !fresh {
		return nil, fmt.Errorf("initial Job status unavailable")
	}
	if err = c.ensureInitialRegistryCustody(ctx, p, dryRun, true); err != nil {
		return nil, err
	}
	if err = c.admitInitialJob(ctx, job, fresh); err != nil {
		return nil, err
	}
	if dryRun {
		receipt := initialJobReceipt(job, true)
		return &receipt, nil
	}
	if p.Revalidate == nil {
		return nil, fmt.Errorf("initial Job authority recheck unavailable")
	}
	if fresh {
		job, err = jobs.Create(ctx, job, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("initial suspended Job creation failed")
		}
	}
	if job.UID == "" {
		return nil, fmt.Errorf("initial Job identity unavailable")
	}
	owner := metav1.OwnerReference{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: ptr.To(true)}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-credentials", Namespace: p.Namespace, Labels: p.jobLabels(), OwnerReferences: []metav1.OwnerReference{owner}}, Type: corev1.SecretTypeOpaque, Data: p.Data}
	secrets := c.kubeClient().CoreV1().Secrets(p.Namespace)
	oldSecret, getErr := secrets.Get(ctx, secret.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(getErr) {
		_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
	} else if getErr != nil || !reflect.DeepEqual(oldSecret.OwnerReferences, secret.OwnerReferences) || !reflect.DeepEqual(oldSecret.Data, secret.Data) {
		err = fmt.Errorf("initial transient custody conflicts")
	}
	if err != nil {
		return nil, fmt.Errorf("initial Job remains suspended: custody unavailable")
	}
	network := initialDatabasePolicy(p, owner)
	networks := c.kubeClient().NetworkingV1().NetworkPolicies(p.Namespace)
	oldNetwork, getErr := networks.Get(ctx, network.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(getErr) {
		_, err = networks.Create(ctx, network, metav1.CreateOptions{})
	} else if getErr != nil || !reflect.DeepEqual(oldNetwork.OwnerReferences, network.OwnerReferences) || !reflect.DeepEqual(oldNetwork.Spec, network.Spec) {
		err = fmt.Errorf("initial network conflicts")
	}
	if err != nil {
		return nil, fmt.Errorf("initial Job remains suspended: network unavailable")
	}
	if err = p.Revalidate(ctx); err != nil {
		return nil, fmt.Errorf("initial Job remains suspended: authority changed")
	}
	if err = c.checkInitialJobNamespace(ctx, p); err != nil {
		return nil, err
	}
	if err = c.ensureInitialRegistryCustody(ctx, p, true, false); err != nil {
		return nil, fmt.Errorf("initial Job remains suspended: registry custody changed")
	}
	// Use the last verified resourceVersion and do not recreate on conflict.
	job.Spec.Suspend = ptr.To(false)
	job, err = jobs.Update(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("initial Job activation uncertain; inspect the same idempotency key before any new operation")
	}
	receipt := initialJobReceipt(job, false)
	return &receipt, nil
}

func initialDatabasePolicy(p IsolatedInitialJobPlan, owner metav1.OwnerReference) *networkingv1.NetworkPolicy {
	port, protocol := intstr.FromInt32(5432), corev1.ProtocolTCP
	return &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: p.jobName() + "-database", Namespace: p.Namespace, Labels: p.jobLabels(), OwnerReferences: []metav1.OwnerReference{owner}}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"enclii.dev/bootstrap-job": p.jobName()}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": p.Cluster}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}}}}}}
}

// Status reads do not rerun producer checks, inspect credentials or retry work.
func (c *Client) IsolatedInitialJobStatus(ctx context.Context, namespace, name string, project, addon uuid.UUID) (*IsolatedInitialJobReceipt, map[string]string, error) {
	if c == nil || c.kubeClient() == nil || project == uuid.Nil || addon == uuid.Nil || namespace != "project-"+project.String()[:8] || len(name) != 32 || !strings.HasPrefix(name, "initial-") {
		return nil, nil, fmt.Errorf("initial Job status scope refused")
	}
	if _, err := hex.DecodeString(name[len("initial-"):]); err != nil {
		return nil, nil, fmt.Errorf("initial Job status name refused")
	}
	job, err := c.kubeClient().BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || job.UID == "" || job.Labels["enclii.dev/managed-by"] != initialJobManager || job.Labels[LabelProjectNamespace] != project.String() || job.Labels["enclii.dev/addon-id"] != addon.String() || job.Labels["enclii.dev/bootstrap-job"] != name {
		return nil, nil, fmt.Errorf("owned initial Job unavailable")
	}
	receipt := initialJobReceipt(job, false)
	return &receipt, map[string]string{"profile": job.Labels["enclii.dev/bootstrap-profile"], "service_id": job.Labels["enclii.dev/service-id"]}, nil
}
