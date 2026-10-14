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
// A refusal names only its fixed stage (load, url, connect [+Prisma code/tls],
// identity, privileged, not owner, memberships, not empty): never a value.
const initialMAPFreshGuard = `let c,w='load';
(async()=>{try{
 const {PrismaClient}=require('/app/node_modules/@prisma/client');c=new PrismaClient();
 w='url';const u=new URL(process.env.DATABASE_URL);
 if(u.username!=='enclii_migrator'||!/^\/[a-z][a-z0-9_]*_sim$/.test(u.pathname))throw Error();
 w='connect';const a=await c.$queryRawUnsafe("SELECT current_database() AS base,current_user AS cuenta,r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication AS privilegiada,d.datdba=r.oid AS propietaria,EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid) AS membresias,(SELECT count(*) FROM pg_tables WHERE schemaname='public') AS tablas FROM pg_roles r JOIN pg_database d ON d.datname=current_database() WHERE r.rolname=current_user");
 w='identity';if(a.length!==1||a[0].cuenta!=='enclii_migrator'||a[0].base!==u.pathname.slice(1))throw Error();
 w='privileged';if(a[0].privilegiada)throw Error();
 w='not owner';if(!a[0].propietaria)throw Error();
 w='memberships';if(a[0].membresias)throw Error();
 w='not empty';if(a[0].tablas!==0n)throw Error();
 process.stdout.write('INITIAL_DATABASE_EMPTY\n');
}catch(e){const k=e&&typeof e.errorCode==='string'&&/^P\d{4}$/.test(e.errorCode)?' '+e.errorCode:'';const t=w==='connect'&&e&&/TLS|certificate/i.test(String(e.message))?' tls':'';process.stderr.write('Initial database guard refused ('+w+k+t+').\n');process.exitCode=1;}finally{if(c)await c.$disconnect().catch(()=>{});}})();`

// Fixed network readiness before any database step. The Job's own egress
// policy is created moments before its Pod, and a new Pod IP must also reach
// the addon's ingress rules; until the CNI programs both, connections are
// refused at once. A once-only Job whose first database step makes a single
// attempt is then held on a transient (observed 2026-10-14: fresh-only exited
// within seconds, with no server-side log). This waits, bounded, for one TCP
// handshake with the host named by the given variable. It neither
// authenticates nor reads data, and prints no part of the URL.
const initialNetworkReadyProbe = `const net=require('net');
let h,p;
try{const u=new URL(process.env[process.argv[1]]);h=u.hostname.replace(/^\[|\]$/g,'');p=Number(u.port||5432);if(!h||!(p>0&&p<65536))throw Error();}
catch{process.stderr.write('Initial network probe refused.\n');process.exit(1);}
const end=Date.now()+120000;let wait=250;
const attempt=()=>{let settled=false;const s=net.connect({host:h,port:p});s.setTimeout(3000);
 const retry=()=>{if(settled)return;settled=true;s.destroy();if(Date.now()>=end){process.stderr.write('Initial network not ready.\n');process.exit(1);}setTimeout(attempt,wait);wait=Math.min(wait*2,3000);};
 s.once('connect',()=>{if(settled)return;settled=true;s.destroy();process.stdout.write('INITIAL_NETWORK_READY\n');process.exit(0);});
 s.once('timeout',retry);s.once('error',retry);};
attempt();`

// A server-built plan: HTTP requests never bind this type. Data is kept out of
// JSON and is transported only to a short-lived Secret owned by the fixed Job.
type IsolatedInitialJobPlan struct {
	ProjectID, AddonID, ServiceID                              uuid.UUID
	Namespace, Cluster, Profile, Image, IdempotencyKey, Reason string
	Data                                                       map[string][]byte           `json:"-"`
	Revalidate                                                 func(context.Context) error `json:"-"`
	Identity                                                   InitialBootstrapIdentity    `json:"-"`
	retainedJobName                                            string
}

type IsolatedInitialJobReceipt struct {
	Name           string                `json:"name"`
	Namespace      string                `json:"namespace"`
	DryRun         bool                  `json:"dry_run"`
	Suspended      bool                  `json:"suspended"`
	Succeeded      int32                 `json:"succeeded"`
	Failed         int32                 `json:"failed"`
	Active         int32                 `json:"active"`
	Native         *InitialNativeReceipt `json:"native_receipt,omitempty"`
	Classification string                `json:"classification"`
}

func (p IsolatedInitialJobPlan) jobName() string {
	if p.retainedJobName != "" {
		return p.retainedJobName
	}
	// One native object name arbitrates concurrent keys. The key hash is retained
	// separately in the intent and must match on all subsequent POST reads.
	sum := sha256.Sum256([]byte(p.ProjectID.String() + "/" + p.AddonID.String() + "/" + p.ServiceID.String() + "/" + p.Profile))
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
	switch {
	case IsMapSimProfile(p.Profile):
		if !strings.HasPrefix(p.Image, "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:") {
			return nil, fmt.Errorf("initial MAP producer mismatch")
		}
		command = []string{"node", "/app/dist/semilla-simulacion.cjs", "--aplicar"}
		migration = []string{"node", "/app/prisma-cli/node_modules/prisma/build/index.js", "migrate", "deploy", "--schema=/app/prisma/schema.prisma"}
		for _, name := range []string{"DATABASE_URL", "DIRECT_DATABASE_URL", "MAP_SIMULATION_RUNTIME_DATABASE_URL", "MAP_SIMULATION_SEED_ENABLED", "MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"} {
			allowed[name] = true
		}
	case p.Profile == initialPortalProfile:
		if !strings.HasPrefix(p.Image, "ghcr.io/madfam-org/crea-sim/gateway@sha256:") {
			return nil, fmt.Errorf("initial portal producer mismatch")
		}
		command = []string{"node", "packages/entrenamiento/src/preparar-custodia-reset.ts"}
		migration = []string{"node", "packages/entrenamiento/src/migrar.ts"}
		allowed["CREA_SIM_MIGRATOR_DATABASE_URL"], allowed["CREA_SIM_PORTAL_DATABASE_URL"], allowed["CREA_SIM_DATABASE_NAMESPACE"] = true, true, true
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
		SecurityContext:        &corev1.SecurityContext{Privileged: ptr.To(false), AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
		TerminationMessagePath: corev1.TerminationMessagePathDefault, TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		Resources:    corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("1Gi")}},
		VolumeMounts: []corev1.VolumeMount{{Name: "ca", MountPath: "/var/run/crea-sim/ca", ReadOnly: true}, {Name: "tmp", MountPath: "/tmp"}}}
	job := &batchv1.Job{TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"}, ObjectMeta: metav1.ObjectMeta{Name: p.jobName(), Namespace: p.Namespace, Labels: p.jobLabels(), Annotations: map[string]string{"enclii.dev/bootstrap-fingerprint": p.fingerprint(), "enclii.dev/operation-reason": p.Reason}},
		Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1), Completions: ptr.To[int32](1), CompletionMode: ptr.To(batchv1.NonIndexedCompletion), PodReplacementPolicy: ptr.To(batchv1.TerminatingOrFailed), Suspend: ptr.To(true), BackoffLimit: ptr.To[int32](0), ActiveDeadlineSeconds: ptr.To[int64](600), TTLSecondsAfterFinished: ptr.To[int32](3600), Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: p.jobLabels()}, Spec: corev1.PodSpec{Tolerations: []corev1.Toleration{
			{Key: corev1.TaintNodeNotReady, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To[int64](300)},
			{Key: corev1.TaintNodeUnreachable, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To[int64](300)},
		}, DNSPolicy: corev1.DNSClusterFirst, SchedulerName: corev1.DefaultSchedulerName, TerminationGracePeriodSeconds: ptr.To[int64](30), EnableServiceLinks: ptr.To(true), ServiceAccountName: "default", DeprecatedServiceAccount: "default", Priority: ptr.To[int32](0), PreemptionPolicy: ptr.To(corev1.PreemptLowerPriority), RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: ptr.To(false), ImagePullSecrets: []corev1.LocalObjectReference{{Name: "ghcr-credentials"}}, SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), FSGroup: ptr.To[int64](1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{container}, Volumes: []corev1.Volume{{Name: "ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: p.jobName() + "-credentials", DefaultMode: ptr.To(corev1.SecretVolumeSourceDefaultMode), Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}}}, {Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}}}}} // pragma: allowlist secret -- fixed Secret references; private values are resolved at runtime
	if len(migration) > 0 && IsMapSimProfile(p.Profile) {
		job.Spec.Template.Spec.SecurityContext.RunAsUser = ptr.To[int64](1001)
		job.Spec.Template.Spec.SecurityContext.RunAsGroup = ptr.To[int64](1001)
		job.Spec.Template.Spec.SecurityContext.FSGroup = ptr.To[int64](1001)
		fresh := container.DeepCopy()
		fresh.Name = "fresh-only"
		fresh.Command = []string{"node", "-e", initialMAPFreshGuard}
		init := container.DeepCopy()
		init.Name = "migrate"
		init.Command = migration
		job.Spec.Template.Spec.InitContainers = []corev1.Container{initialNetworkReady(container, "DATABASE_URL"), *fresh, *init}
	}
	if p.Profile == initialPortalProfile {
		init := container.DeepCopy()
		init.Name = "portal001"
		init.Command = migration
		job.Spec.Template.Spec.InitContainers = []corev1.Container{initialNetworkReady(container, "CREA_SIM_MIGRATOR_DATABASE_URL"), *init}
	}
	return job, nil
}

// initialNetworkReady runs first, with the same image, identity and limits as
// the profile's own containers; only the fixed probe and the variable it reads
// differ.
func initialNetworkReady(base corev1.Container, variable string) corev1.Container {
	ready := base.DeepCopy()
	ready.Name = "network-ready"
	ready.Command = []string{"node", "-e", initialNetworkReadyProbe, variable}
	return *ready
}

func initialJobReceipt(job *batchv1.Job, dryRun bool) IsolatedInitialJobReceipt {
	return IsolatedInitialJobReceipt{Name: job.Name, Namespace: job.Namespace, DryRun: dryRun, Suspended: job.Spec.Suspend != nil && *job.Spec.Suspend, Succeeded: job.Status.Succeeded, Failed: job.Status.Failed, Active: job.Status.Active, Classification: "INITIAL_ONLY_EXECUTION"}
}

func (c *Client) checkInitialJobNamespace(ctx context.Context, p IsolatedInitialJobPlan) error {
	if c == nil || c.kubeClient() == nil || c.DynamicClient == nil {
		return fmt.Errorf("initial Job adapter unavailable")
	}
	ns, err := c.kubeClient().CoreV1().Namespaces().Get(ctx, p.Namespace, metav1.GetOptions{})
	if err != nil || ns.UID == "" || ns.Labels[LabelProjectNamespace] != p.ProjectID.String() || ns.Labels["enclii.dev/verify-signatures"] != "true" || string(ns.UID) != p.Identity.AddonNamespaceUID {
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

// Create suspended first, bind its observed UID in immutable namespace intent,
// prepare owned transient custody/network, recheck
// authority, then activate once. A timeout never starts an automatic retry.
func (c *Client) RunIsolatedInitialJob(ctx context.Context, p IsolatedInitialJobPlan, dryRun bool) (*IsolatedInitialJobReceipt, error) {
	job, err := BuildIsolatedInitialJob(p)
	if err != nil {
		return nil, err
	}
	if err = c.checkInitialJobNamespace(ctx, p); err != nil {
		return nil, err
	}
	intent, err := p.initialIntent(job)
	if err != nil {
		return nil, err
	}
	if err = c.checkInitialOperationExclusion(ctx, p); err != nil {
		return nil, err
	}
	jobs := c.kubeClient().BatchV1().Jobs(p.Namespace)
	retained, intentErr := c.readInitialIntent(ctx, p, intent)
	existing, jobErr := jobs.Get(ctx, job.Name, metav1.GetOptions{})
	if intentErr == nil {
		if DecodeInitialBootstrapJSON([]byte(retained.Data["payload"]), &intent) != nil {
			return nil, fmt.Errorf("initial intent refused")
		}
		native, receiptErr := c.readInitialReceipt(ctx, p, retained)
		if receiptErr == nil {
			return &IsolatedInitialJobReceipt{Name: job.Name, Namespace: p.Namespace, Succeeded: 1, Native: native, Classification: "INITIAL_ONLY", DryRun: dryRun}, nil
		}
		if !k8serrors.IsNotFound(receiptErr) {
			return nil, receiptErr
		}
		if jobErr != nil || !initialJobMatches(existing, job, intent) {
			return nil, fmt.Errorf("initial intent has no matching Job; held, no retry")
		}
		if existing.Spec.Suspend == nil || *existing.Spec.Suspend || existing.Status.Failed > 0 {
			return nil, fmt.Errorf("initial Job partial or failed; held, no repair")
		}
		receipt := initialJobReceipt(existing, dryRun)
		if existing.Status.Succeeded > 0 && !dryRun {
			if p.Revalidate == nil || p.Revalidate(ctx) != nil {
				return nil, fmt.Errorf("initial collection authority changed")
			}
			receipt.Native, err = c.collectInitialReceipt(ctx, p, existing, job, retained, intent)
			if err != nil {
				return nil, err
			}
			receipt.Classification = "INITIAL_ONLY"
		}
		return &receipt, nil
	}
	if !k8serrors.IsNotFound(intentErr) {
		return nil, fmt.Errorf("initial retained intent unavailable or conflicting")
	}
	if !k8serrors.IsNotFound(jobErr) {
		return nil, fmt.Errorf("existing or unknown Job without retained intent; held")
	}
	if err = c.ensureInitialRegistryCustody(ctx, p, true, true); err != nil {
		return nil, err
	}
	if err = c.admitInitialJob(ctx, job, true); err != nil {
		return nil, err
	}
	record, err := initialRecord(p, "-intent", intent)
	if err != nil {
		return nil, err
	}
	if _, err = c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Create(ctx, record, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}); err != nil {
		return nil, fmt.Errorf("initial intent admission refused")
	}
	if dryRun {
		receipt := initialJobReceipt(job, true)
		return &receipt, nil
	}
	if p.Revalidate == nil {
		return nil, fmt.Errorf("initial Job authority recheck unavailable")
	}

	expected := job.DeepCopy()
	name := job.Name
	job, err = jobs.Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		_, _ = jobs.Get(ctx, name, metav1.GetOptions{})
		return nil, fmt.Errorf("initial Job creation uncertain; read same operation, no retry")
	}
	if job.UID == "" || job.ResourceVersion == "" || !initialJobMatches(job, expected, intent) {
		return nil, fmt.Errorf("initial suspended Job identity/spec unavailable; held")
	}
	intent.JobUID = string(job.UID)
	intent.CreationResourceVersion = job.ResourceVersion
	record, err = initialRecord(p, "-intent", intent)
	if err != nil {
		return nil, err
	}
	retained, err = c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Create(ctx, record, metav1.CreateOptions{})
	if err != nil {
		_, _ = c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Get(ctx, record.Name, metav1.GetOptions{})
		return nil, fmt.Errorf("initial intent creation uncertain; read same operation, no retry")
	}
	var kept initialIntent
	if validateInitialRecord(retained, p, "-intent", &kept) != nil || !reflect.DeepEqual(kept, intent) {
		return nil, fmt.Errorf("initial intent retention not accredited")
	}
	if err = c.ensureInitialRegistryCustody(ctx, p, false, true); err != nil {
		return nil, err
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
	job, err = c.activateInitialJob(ctx, p, job, expected, intent, retained)
	if err != nil {
		return nil, err
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
	return c.retainedInitialStatus(ctx, namespace, name, project, addon)
}

// Activation is one CAS update on the last authoritative read, never a retry.
func (c *Client) activateInitialJob(ctx context.Context, p IsolatedInitialJobPlan, job, expected *batchv1.Job, intent initialIntent, retained *corev1.ConfigMap) (*batchv1.Job, error) {
	jobs := c.kubeClient().BatchV1().Jobs(p.Namespace)
	// Read the same UID and fixed spec immediately before the one CAS PUT.
	// The original creation RV stays historical; controller updates may advance it.
	current, e := jobs.Get(ctx, job.Name, metav1.GetOptions{})
	if e != nil || !initialJobMatches(current, expected, intent) || current.Spec.Suspend == nil || !*current.Spec.Suspend || current.Status.Active != 0 || current.Status.Succeeded != 0 || current.Status.Failed != 0 {
		return nil, fmt.Errorf("initial suspended Job changed; held")
	}
	currentIntent, e := c.readInitialIntent(ctx, p, intent)
	if e != nil || currentIntent.UID != retained.UID {
		return nil, fmt.Errorf("initial retained intent changed; held")
	}
	job = current
	job.Spec.Suspend = ptr.To(false)
	job, err := jobs.Update(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("initial Job activation uncertain; inspect the same idempotency key before any new operation")
	}
	return job, nil
}
