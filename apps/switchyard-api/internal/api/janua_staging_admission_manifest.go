package api

import (
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Reviewed catalog descriptor, not a claim that this image has run anywhere.
// Source: cloudnative-pg/artifacts@61f84bd4493da4e92fce41b79093246ad266a25d,
// image-catalogs/catalog-standard-trixie.yaml (major 18).
const januaStagePostgresImage = types.JanuaStagingPostgresImage
const januaStageClusterName = "pg-janua-staging-db"
const januaStageAdmissionPod = "janua-staging-db-admission"

var januaStageClusterGVR = schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}

type januaStageAdmissionObjects struct {
	Cluster  *unstructured.Unstructured
	Policies []*networkingv1.NetworkPolicy
	Pod      *corev1.Pod
}

// No caller-supplied image, SQL, role, namespace, credential or generic addon
// Config reaches this closed builder. Secret names are references only; no
// Secret read/generation/create or owner credential is part of admission.
func januaStageAdmissionManifests(project uuid.UUID, service *types.Service, plan *db.ManagedDBPlan, peers []januaStageAPIPeer, operator metav1.LabelSelector) (januaStageAdmissionObjects, error) {
	if project == uuid.Nil || service == nil || service.ID == uuid.Nil || service.ProjectID != project || len(validation.IsDNS1123Label(service.Name)) != 0 || !januaStageCatalogPlanValid(plan) || plan.MaxConnections <= 0 || len(peers) == 0 || len(operator.MatchLabels) == 0 || len(operator.MatchExpressions) != 0 {
		return januaStageAdmissionObjects{}, errJanuaStageAdmission
	}
	contract := fixedJanuaStageDatabaseContract()
	namespace := "project-" + project.String()[:8]
	labels := map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/managed-by": "janua-staging-admission", "enclii.dev/profile": contract.Profile, "app.kubernetes.io/name": "janua-staging-db"}
	metadata := func(name, ns string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}
	}
	cluster := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster",
		"metadata": map[string]any{"name": januaStageClusterName, "namespace": namespace, "labels": stringMapAny(labels)},
		"spec": map[string]any{
			"instances": int64(plan.ReplicaCount), "imageName": januaStagePostgresImage,
			"enableSuperuserAccess": false, "primaryUpdateStrategy": "supervised",
			"storage":    map[string]any{"size": fmt.Sprintf("%dGi", plan.StorageGB), "storageClass": contract.StorageClass},
			"resources":  map[string]any{"requests": map[string]any{"cpu": plan.CPURequest, "memory": plan.MemoryRequest}, "limits": map[string]any{"cpu": plan.CPURequest, "memory": plan.MemoryRequest}},
			"postgresql": map[string]any{"parameters": map[string]any{"max_connections": strconv.Itoa(plan.MaxConnections), "ssl_min_protocol_version": "TLSv1.2"}},
			"bootstrap": map[string]any{"initdb": map[string]any{
				"database": contract.DatabaseName, "owner": contract.MigratorRole,
				"postInitApplicationSQL": []any{
					`CREATE ROLE "janua_staging_runtime" NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT`,
					`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
					`REVOKE TEMPORARY ON DATABASE "janua_staging" FROM PUBLIC`,
					`GRANT CONNECT ON DATABASE "janua_staging" TO "janua_staging_runtime"`,
					`GRANT USAGE ON SCHEMA public TO "janua_staging_runtime"`,
				},
			}},
			"managed": map[string]any{"roles": []any{map[string]any{
				"name": contract.RuntimeRole, "ensure": "present", "login": true,
				"superuser": false, "bypassrls": false, "createdb": false, "createrole": false, "replication": false, "inherit": false,
				"passwordSecret": map[string]any{"name": januaStageClusterName + "-runtime"},
			}}},
		},
	}}
	clusterSelector := metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": januaStageClusterName}}
	stageSelector := metav1.LabelSelector{MatchLabels: map[string]string{"enclii.dev/service": service.Name}}
	namespaceSelector := func(name string) *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": name, k8s.LabelProjectNamespace: project.String()}}
	}
	port := func(value int32) []networkingv1.NetworkPolicyPort {
		protocol, p := corev1.ProtocolTCP, intstr.FromInt32(value)
		return []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &p}}
	}
	clusterPeer := networkingv1.NetworkPolicyPeer{PodSelector: &clusterSelector}
	apiRules := make([]networkingv1.NetworkPolicyEgressRule, 0, len(peers))
	for _, peer := range peers {
		apiRules = append(apiRules, networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: peer.HostRoute}}}, Ports: port(peer.Port)})
	}
	apiRules = append(apiRules, networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{clusterPeer}, Ports: port(5432)})
	databasePolicy := &networkingv1.NetworkPolicy{ObjectMeta: metadata("janua-staging-db-isolation", namespace), Spec: networkingv1.NetworkPolicySpec{
		PodSelector: clusterSelector, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		Ingress: []networkingv1.NetworkPolicyIngressRule{
			{From: []networkingv1.NetworkPolicyPeer{clusterPeer}, Ports: port(5432)},
			{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: namespaceSelector("janua-staging"), PodSelector: &stageSelector}}, Ports: port(5432)},
			{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "cnpg-system"}}, PodSelector: &operator}}, Ports: port(8000)},
		}, Egress: apiRules,
	}}
	consumerPolicy := &networkingv1.NetworkPolicy{ObjectMeta: metadata("janua-staging-api-database", "janua-staging"), Spec: networkingv1.NetworkPolicySpec{
		PodSelector: stageSelector, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: namespaceSelector(namespace), PodSelector: &clusterSelector}}, Ports: port(5432)}},
	}}
	// This representative Pod only exercises image/security admission. It has
	// no credentials, volumes, SQL, owner UID, network action or running side
	// effect; DryRunAll is mandatory in the only submission path.
	no, yes, uid := false, true, int64(26)
	pod := &corev1.Pod{ObjectMeta: metadata(januaStageAdmissionPod, namespace), Spec: corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: &no,
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		Containers: []corev1.Container{{Name: "image-admission", Image: januaStagePostgresImage, ImagePullPolicy: corev1.PullIfNotPresent, Command: []string{"postgres"}, Args: []string{"--version"},
			Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(plan.CPURequest), corev1.ResourceMemory: resource.MustParse(plan.MemoryRequest)}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(plan.CPURequest), corev1.ResourceMemory: resource.MustParse(plan.MemoryRequest)}},
			SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
		}},
	}}
	return januaStageAdmissionObjects{Cluster: cluster, Policies: []*networkingv1.NetworkPolicy{databasePolicy, consumerPolicy}, Pod: pod}, nil
}

func stringMapAny(values map[string]string) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
