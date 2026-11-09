package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

type januaAdmissionFixture struct {
	h                                   *Handler
	mock                                sqlmock.Sqlmock
	client                              *fake.Clientset
	dynamic                             *dynamicfake.FakeDynamicClient
	project, env, registration, service uuid.UUID
	path                                string
}

func newJanuaAdmissionFixture(t *testing.T) *januaAdmissionFixture {
	t.Helper()
	h, m, c, path := januaStagePlanFixture(t, "janua-staging", false)
	query, err := url.Parse(path)
	require.NoError(t, err)
	s, err := uuid.Parse(query.Query().Get("service_id"))
	require.NoError(t, err)
	namespaces, err := c.CoreV1().Namespaces().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	p, err := uuid.Parse(namespaces.Items[0].Labels[k8s.LabelProjectNamespace])
	require.NoError(t, err)
	storage, err := c.StorageV1().StorageClasses().Get(t.Context(), fixedJanuaStageDatabaseContract().StorageClass, metav1.GetOptions{})
	require.NoError(t, err)
	storage.ResourceVersion = "1"
	_, err = c.StorageV1().StorageClasses().Update(t.Context(), storage, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = c.CoreV1().Services("default").Create(t.Context(), &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default", UID: "api-service", ResourceVersion: "1", Labels: map[string]string{"component": "apiserver", "provider": "kubernetes"}}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "192.0.2.1", ClusterIPs: []string{"192.0.2.1"}, Ports: []corev1.ServicePort{{Name: "https", Protocol: corev1.ProtocolTCP, Port: 443, TargetPort: intstr.FromInt32(6443)}}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.CoreV1().Endpoints("default").Create(t.Context(), &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default", UID: "api-endpoints", ResourceVersion: "1"}, Subsets: []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{{IP: "198.51.100.2"}}, Ports: []corev1.EndpointPort{{Name: "https", Protocol: corev1.ProtocolTCP, Port: 6443}}}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cnpg-system", UID: "operator-namespace", ResourceVersion: "1", Labels: map[string]string{"kubernetes.io/metadata.name": "cnpg-system"}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.AppsV1().Deployments("cnpg-system").Create(t.Context(), &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: "cnpg-system", UID: "operator-deployment", ResourceVersion: "1", Generation: 1, Labels: map[string]string{"app.kubernetes.io/name": "cloudnative-pg"}}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "cloudnative-pg", "app.kubernetes.io/instance": "operator"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/name": "cloudnative-pg", "app.kubernetes.io/instance": "operator"}}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, AvailableReplicas: 1}}, metav1.CreateOptions{})
	require.NoError(t, err)
	d := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{januaStageClusterGVR: "ClusterList"})
	h.k8sClient.DynamicClient = d
	c.ClearActions()
	return &januaAdmissionFixture{h: h, mock: m, client: c, dynamic: d, project: p, service: s, path: strings.Replace(path, "addon-plan", "addon-admission", 1)}
}

func TestJanuaStageAdmissionSourceRequiresExactMetadataAndNoCredentialReads(t *testing.T) {
	f := newJanuaAdmissionFixture(t)
	s, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
	require.NoError(t, err)
	require.Equal(t, f.project, s.ProjectID)
	require.Equal(t, []januaStageAPIPeer{{HostRoute: "192.0.2.1/32", Port: 443}, {HostRoute: "198.51.100.2/32", Port: 6443}}, s.Peers)
	for _, action := range f.client.Actions() {
		require.Contains(t, []string{"get", "list"}, action.GetVerb())
		require.NotEqual(t, "secrets", action.GetResource().Resource)
	}
	for _, action := range f.dynamic.Actions() {
		require.Equal(t, "list", action.GetVerb())
	}
	before := januaStageSourceFingerprint(s)
	s.Service.BuildConfig.Dockerfile = "ignored-build-config"
	require.Equal(t, before, januaStageSourceFingerprint(s))
	s.EndpointVersions[1] = "changed"
	require.NotEqual(t, before, januaStageSourceFingerprint(s))
}

func TestJanuaStageAdmissionForeignSourceRefusesBeforeNative(t *testing.T) {
	for _, foreignEnvironment := range []bool{false, true} {
		h, _, client, path := januaStagePlanFixture(t, "janua", foreignEnvironment)
		parsed, err := url.Parse(path)
		require.NoError(t, err)
		service, _ := uuid.Parse(parsed.Query().Get("service_id"))
		_, err = h.readJanuaStageAdmissionSource(t.Context(), service, "standard-0")
		require.Error(t, err)
		require.Empty(t, client.Actions())
	}
}

func TestJanuaStageAdmissionIsolationFailuresPrecedeProducerAndDryRun(t *testing.T) {
	for _, failure := range []string{"broad-data-label", "broad-consumer-label", "other-policy", "baseline-drift", "storage-delete", "storage-version-missing", "existing-cluster", "partial-policy-list", "terminating-namespace"} {
		t.Run(failure, func(t *testing.T) {
			f := newJanuaAdmissionFixture(t)
			switch failure {
			case "broad-data-label", "broad-consumer-label":
				n, err := f.client.CoreV1().Namespaces().Get(t.Context(), "janua-staging", metav1.GetOptions{})
				require.NoError(t, err)
				key := "enclii.dev/data-access"
				if failure == "broad-consumer-label" {
					key = "enclii.dev/postgres-consumer"
				}
				n.Labels[key] = "true"
				_, err = f.client.CoreV1().Namespaces().Update(t.Context(), n, metav1.UpdateOptions{})
				require.NoError(t, err)
			case "other-policy":
				_, err := f.client.NetworkingV1().NetworkPolicies("janua-staging").Create(t.Context(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "unreviewed", Namespace: "janua-staging"}}, metav1.CreateOptions{})
				require.NoError(t, err)
			case "baseline-drift":
				p, err := f.client.NetworkingV1().NetworkPolicies("janua-staging").Get(t.Context(), "default-deny", metav1.GetOptions{})
				require.NoError(t, err)
				p.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{}}
				_, err = f.client.NetworkingV1().NetworkPolicies("janua-staging").Update(t.Context(), p, metav1.UpdateOptions{})
				require.NoError(t, err)
			case "storage-delete", "storage-version-missing":
				s, err := f.client.StorageV1().StorageClasses().Get(t.Context(), fixedJanuaStageDatabaseContract().StorageClass, metav1.GetOptions{})
				require.NoError(t, err)
				if failure == "storage-delete" {
					policy := corev1.PersistentVolumeReclaimDelete
					s.ReclaimPolicy = &policy
				} else {
					s.ResourceVersion = ""
				}
				_, err = f.client.StorageV1().StorageClasses().Update(t.Context(), s, metav1.UpdateOptions{})
				require.NoError(t, err)
			case "existing-cluster":
				require.NoError(t, f.dynamic.Tracker().Add(&unstructured.Unstructured{Object: map[string]any{"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster", "metadata": map[string]any{"name": "existing", "namespace": "project-" + f.project.String()[:8]}}}))
			case "partial-policy-list":
				f.client.PrependReactor("list", "networkpolicies", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, &networkingv1.NetworkPolicyList{ListMeta: metav1.ListMeta{Continue: "unread-policy-page"}}, nil
				})
			case "terminating-namespace":
				n, err := f.client.CoreV1().Namespaces().Get(t.Context(), "janua-staging", metav1.GetOptions{})
				require.NoError(t, err)
				now := metav1.Now()
				n.DeletionTimestamp = &now
				_, err = f.client.CoreV1().Namespaces().Update(t.Context(), n, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			f.client.ClearActions()
			f.dynamic.ClearActions()
			_, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
			require.Error(t, err)
			for _, action := range append(f.client.Actions(), f.dynamic.Actions()...) {
				require.Contains(t, []string{"get", "list"}, action.GetVerb())
				require.NotEqual(t, "secrets", action.GetResource().Resource)
			}
		})
	}
}

func TestJanuaStageAPIEndpointRefusesUnknownAndIncompleteNativeMetadata(t *testing.T) {
	for _, failure := range []string{"not-ready", "hostname", "loopback", "wrong-port", "missing-version", "external-service", "wrong-provider", "truncated"} {
		t.Run(failure, func(t *testing.T) {
			// Only native endpoint readers are exercised; consume the SQL fixture's
			// expected source reads once before mutating infrastructure metadata.
			f := newJanuaAdmissionFixture(t)
			_, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
			require.NoError(t, err)
			e, err := f.client.CoreV1().Endpoints("default").Get(t.Context(), "kubernetes", metav1.GetOptions{})
			require.NoError(t, err)
			s, err := f.client.CoreV1().Services("default").Get(t.Context(), "kubernetes", metav1.GetOptions{})
			require.NoError(t, err)
			switch failure {
			case "not-ready":
				e.Subsets[0].NotReadyAddresses = []corev1.EndpointAddress{{IP: "198.51.100.3"}}
			case "hostname":
				e.Subsets[0].Addresses[0].IP = "host.example.invalid"
			case "loopback":
				e.Subsets[0].Addresses[0].IP = "127.0.0.1"
			case "wrong-port":
				e.Subsets[0].Ports[0].Port = 1234
			case "missing-version":
				e.ResourceVersion = ""
			case "external-service":
				s.Spec.ExternalIPs = []string{"203.0.113.1"}
			case "wrong-provider":
				s.Labels["provider"] = "other"
			case "truncated":
				e.Annotations = map[string]string{"endpoints.kubernetes.io/over-capacity": "truncated"}
			}
			_, err = f.client.CoreV1().Endpoints("default").Update(t.Context(), e, metav1.UpdateOptions{})
			require.NoError(t, err)
			_, err = f.client.CoreV1().Services("default").Update(t.Context(), s, metav1.UpdateOptions{})
			require.NoError(t, err)
			_, _, err = f.h.readJanuaStageAPIPeers(t.Context())
			require.Error(t, err)
		})
	}
}

func TestJanuaStageAdmissionOperatorSelectorRequiresCurrentAvailableOwner(t *testing.T) {
	for _, failure := range []string{"unavailable", "stale", "template-mismatch", "unknown-selector", "host-network"} {
		t.Run(failure, func(t *testing.T) {
			f := newJanuaAdmissionFixture(t)
			_, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
			require.NoError(t, err)
			d, err := f.client.AppsV1().Deployments("cnpg-system").Get(t.Context(), "operator", metav1.GetOptions{})
			require.NoError(t, err)
			switch failure {
			case "unavailable":
				d.Status.AvailableReplicas = 0
			case "stale":
				d.Status.ObservedGeneration = 0
			case "template-mismatch":
				d.Spec.Template.Labels["app.kubernetes.io/instance"] = "other"
			case "unknown-selector":
				d.Spec.Selector.MatchExpressions = []metav1.LabelSelectorRequirement{{Key: "any", Operator: metav1.LabelSelectorOpExists}}
			case "host-network":
				d.Spec.Template.Spec.HostNetwork = true
			}
			_, err = f.client.AppsV1().Deployments("cnpg-system").Update(t.Context(), d, metav1.UpdateOptions{})
			require.NoError(t, err)
			_, _, err = f.h.readJanuaStageOperator(t.Context())
			require.Error(t, err)
		})
	}
}

func TestJanuaStageManifestsKeepClosedAuthorityAndNoBroadNetwork(t *testing.T) {
	f := newJanuaAdmissionFixture(t)
	s, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
	require.NoError(t, err)
	objects, err := januaStageAdmissionManifests(s.ProjectID, s.Service, s.Plan, s.Peers, s.OperatorSelector)
	require.NoError(t, err)
	encoded, err := json.Marshal(objects.Cluster)
	require.NoError(t, err)
	require.Contains(t, string(encoded), januaStagePostgresImage)
	require.NotContains(t, string(encoded), "barmanObjectStore")
	require.Contains(t, string(encoded), `"owner":"enclii_migrator"`)
	require.Contains(t, string(encoded), `"name":"janua_staging_runtime"`)
	require.Contains(t, string(encoded), `"bypassrls":false`)
	require.Contains(t, string(encoded), `"superuser":false`)
	require.NotContains(t, string(encoded), `"password":`)
	for _, key := range []string{"postInitSQL", "postInitTemplateSQL", "postInitApplicationSQLRefs"} {
		mutated := objects.Cluster.DeepCopy()
		require.NoError(t, unstructured.SetNestedField(mutated.Object, []any{"unreviewed-authority"}, "spec", "bootstrap", "initdb", key))
		require.False(t, januaStageAdmissionAuthorityClosed(objects.Cluster, mutated))
	}
	mutatedCluster := objects.Cluster.DeepCopy()
	roles, _, err := unstructured.NestedSlice(mutatedCluster.Object, "spec", "managed", "roles")
	require.NoError(t, err)
	roles[0].(map[string]any)["inRoles"] = []any{"postgres"}
	require.NoError(t, unstructured.SetNestedSlice(mutatedCluster.Object, roles, "spec", "managed", "roles"))
	require.False(t, januaStageAdmissionAuthorityClosed(objects.Cluster, mutatedCluster))
	require.Error(t, addons.ValidatePostgresRuntimeConfig(types.DatabaseAddonTypePostgres, types.DatabaseAddonConfig{DatabaseName: "janua_staging", RuntimeRole: "janua_staging_runtime"}))
	for _, policy := range objects.Policies {
		for _, rule := range policy.Spec.Egress {
			require.NotEmpty(t, rule.To)
			for _, peer := range rule.To {
				if peer.IPBlock != nil {
					require.Contains(t, []string{"192.0.2.1/32", "198.51.100.2/32"}, peer.IPBlock.CIDR)
				}
			}
		}
	}
	consumer := objects.Policies[1]
	require.Equal(t, map[string]string{"enclii.dev/service": "api"}, consumer.Spec.PodSelector.MatchLabels)
	require.Equal(t, s.ProjectID.String(), consumer.Spec.Egress[0].To[0].NamespaceSelector.MatchLabels[k8s.LabelProjectNamespace])
	require.Empty(t, objects.Pod.Spec.Volumes)
	require.Empty(t, objects.Pod.Spec.Containers[0].Env)
	require.Equal(t, []string{"--version"}, objects.Pod.Spec.Containers[0].Args)
	require.True(t, januaStageAdmissionPodSafe(objects.Pod, objects.Pod.DeepCopy()))
	mutated := objects.Pod.DeepCopy()
	mutated.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "UNEXPECTED", Value: "fixture"}}
	require.False(t, januaStageAdmissionPodSafe(objects.Pod, mutated))
	mutated = objects.Pod.DeepCopy()
	mutated.Spec.AutomountServiceAccountToken = nil
	require.False(t, januaStageAdmissionPodSafe(objects.Pod, mutated))
	for _, field := range []string{"request", "limit"} {
		mutated = objects.Pod.DeepCopy()
		if field == "request" {
			mutated.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("1Gi")
		} else {
			mutated.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("2")
		}
		require.False(t, januaStageAdmissionPodSafe(objects.Pod, mutated))
	}
}

func dryJanuaAdmissionReactors(t *testing.T, f *januaAdmissionFixture, failAt int, change func()) *int {
	t.Helper()
	count := 0
	reactor := func(action ktesting.Action) (bool, runtime.Object, error) {
		count++
		create, ok := action.(ktesting.CreateAction)
		require.True(t, ok)
		options, ok := action.(interface{ GetCreateOptions() metav1.CreateOptions })
		require.True(t, ok)
		require.Equal(t, []string{metav1.DryRunAll}, options.GetCreateOptions().DryRun)
		require.Equal(t, metav1.FieldValidationStrict, options.GetCreateOptions().FieldValidation)
		require.Contains(t, []string{"clusters", "networkpolicies", "pods"}, action.GetResource().Resource)
		if count == failAt {
			return true, nil, errors.New("private upstream fixture must never escape")
		}
		if count == 4 && change != nil {
			change()
		}
		return true, create.GetObject().DeepCopyObject(), nil
	}
	f.client.PrependReactor("create", "*", reactor)
	f.dynamic.PrependReactor("create", "*", reactor)
	return &count
}

func TestJanuaStageNativeAdmissionAlwaysDryRunAndStopsAtFirstFailure(t *testing.T) {
	for _, failAt := range []int{0, 1, 2, 3, 4} {
		t.Run(strconv.Itoa(failAt), func(t *testing.T) {
			f := newJanuaAdmissionFixture(t)
			s, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
			require.NoError(t, err)
			objects, err := januaStageAdmissionManifests(s.ProjectID, s.Service, s.Plan, s.Peers, s.OperatorSelector)
			require.NoError(t, err)
			count := dryJanuaAdmissionReactors(t, f, failAt, nil)
			admitted, err := f.h.dryAdmitJanuaStage(t.Context(), objects)
			if failAt == 0 {
				require.NoError(t, err)
				require.Equal(t, 4, admitted)
				require.Equal(t, 4, *count)
			} else {
				require.Error(t, err)
				require.Equal(t, failAt, *count)
			}
			_, err = f.client.CoreV1().Pods(objects.Pod.Namespace).Get(t.Context(), objects.Pod.Name, metav1.GetOptions{})
			require.True(t, apierrors.IsNotFound(err))
			clusters, err := f.dynamic.Resource(januaStageClusterGVR).Namespace(objects.Cluster.GetNamespace()).List(t.Context(), metav1.ListOptions{})
			require.NoError(t, err)
			require.Empty(t, clusters.Items)
		})
	}
}

func TestJanuaStageAdmissionRejectsMutatedCriticalNativeReceipt(t *testing.T) {
	for _, target := range []string{"clusters", "networkpolicies", "pods"} {
		t.Run(target, func(t *testing.T) {
			f := newJanuaAdmissionFixture(t)
			s, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
			require.NoError(t, err)
			objects, err := januaStageAdmissionManifests(s.ProjectID, s.Service, s.Plan, s.Peers, s.OperatorSelector)
			require.NoError(t, err)
			dryJanuaAdmissionReactors(t, f, 0, nil)
			mutate := func(action ktesting.Action) (bool, runtime.Object, error) {
				object := action.(ktesting.CreateAction).GetObject().DeepCopyObject()
				switch result := object.(type) {
				case *unstructured.Unstructured:
					require.NoError(t, unstructured.SetNestedField(result.Object, "unreviewed-image", "spec", "imageName"))
				case *networkingv1.NetworkPolicy:
					result.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
				case *corev1.Pod:
					yes := true
					result.Spec.AutomountServiceAccountToken = &yes
				}
				return true, object, nil
			}
			if target == "clusters" {
				f.dynamic.PrependReactor("create", target, mutate)
			} else {
				f.client.PrependReactor("create", target, mutate)
			}
			_, err = f.h.dryAdmitJanuaStage(t.Context(), objects)
			require.Error(t, err)
		})
	}
}

func TestJanuaStageProducerArgumentsAndDigestAreClosed(t *testing.T) {
	args := januaStagePostgresSignatureArgs()
	require.Equal(t, januaStagePostgresImage, args[len(args)-1])
	require.Contains(t, args, "cloudnative-pg/postgres-containers")
	_, digest, _ := strings.Cut(januaStagePostgresImage, "@")
	require.NoError(t, januaStageSignatureDigestMatches([]byte(`[{"critical":{"image":{"docker-manifest-digest":"`+digest+`"}}}]`)))
	for _, body := range []string{`[]`, `not-json`, `[{"critical":{"image":{"docker-manifest-digest":"sha256:other"}}}]`} {
		require.Error(t, januaStageSignatureDigestMatches([]byte(body)))
	}
	identity := regexp.MustCompile(januaStagePostgresSigner)
	require.True(t, identity.MatchString("https://github.com/cloudnative-pg/postgres-containers/.github/workflows/bake.yml@refs/pull/1/merge"))
	for _, value := range []string{"https://github.com/other/postgres-containers/.github/workflows/bake.yml@refs/heads/main", "https://github.com/cloudnative-pg/postgres-containers-other/.github/workflows/bake.yml@refs/heads/main", "https://githubXcom/cloudnative-pg/postgres-containers/.github/workflows/bake.yml@refs/heads/main"} {
		require.False(t, identity.MatchString(value))
	}
	buffer := &januaStageSignatureBuffer{}
	_, err := buffer.Write(make([]byte, 1024*1024+1))
	require.Error(t, err)
	require.Zero(t, buffer.Len())
	environment := januaStagePublicVerifierEnv([]string{"PATH=/usr/local/bin", "SSL_CERT_FILE=/public/ca.crt", "GITHUB_TOKEN=private-fixture", "AWS_ACCESS_KEY_ID=private-fixture", "REGISTRY_AUTH_FILE=/private/auth.json", "DOCKER_CONFIG=/private/docker", "HTTPS_PROXY=private-fixture"}, "/private-temp-fixture")
	joined := strings.Join(environment, "\n")
	for _, value := range []string{"GITHUB_TOKEN", "AWS_ACCESS_KEY_ID", "REGISTRY_AUTH_FILE", "/private/auth.json", "/private/docker", "HTTPS_PROXY", "private-fixture"} {
		require.NotContains(t, joined, value)
	}
	require.Contains(t, environment, "PATH=/usr/local/bin")
	require.Contains(t, environment, "DOCKER_CONFIG=/private-temp-fixture")
	require.Contains(t, environment, "HOME=/private-temp-fixture")
	require.Contains(t, environment, "XDG_CONFIG_HOME=/private-temp-fixture")
}

func TestJanuaStageAdmissionRouteRefusesSelectorsAndTenantAdmin(t *testing.T) {
	for _, query := range []string{"?plan=standard-0", "?plan=standard-0&service_id=bad", "?plan=standard-0&service_id=" + uuid.NewString() + "&apply=true", "?plan=standard-0&plan=standard-1&service_id=" + uuid.NewString()} {
		gin.SetMode(gin.TestMode)
		h := &Handler{auth: &auth.JWTManager{}}
		router := gin.New()
		router.POST("/projects/:slug/environments/staging/addon-admission", h.JanuaStagingAddonAdmission)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/projects/janua/environments/staging/addon-admission"+query, nil))
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, operator := range []bool{false, true} {
		database, mock, err := sqlmock.New()
		require.NoError(t, err)
		user := uuid.New()
		mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(operator))
		h := &Handler{auth: &auth.JWTManager{}, repos: &db.Repositories{TenantScope: db.NewTenantScopeRepository(database)}, logger: testLogger(t)}
		router := gin.New()
		router.Use(withUserContext(user, "admin"), func(c *gin.Context) { c.Set("user_role", "admin"); c.Next() })
		h.registerAdminRoutes(router.Group("/v1"))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/admin/projects/janua/environments/staging/addon-admission?plan=standard-0&service_id="+uuid.NewString(), nil))
		want := http.StatusForbidden
		if operator {
			want = http.StatusServiceUnavailable
		}
		require.Equal(t, want, response.Code)
		require.NoError(t, mock.ExpectationsWereMet())
		mock.ExpectClose()
		require.NoError(t, database.Close())
	}
}

func expectJanuaAdmissionProject(mock sqlmock.Sqlmock, source *januaStageAdmissionSource, now time.Time) {
	mock.ExpectQuery(`FROM projects WHERE slug`).WithArgs("janua").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(source.ProjectID, "Fixture", "janua", "", now, now))
}

func expectJanuaAdmissionSource(mock sqlmock.Sqlmock, s *januaStageAdmissionSource) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	expectJanuaAdmissionProject(mock, s, now)
	mock.ExpectQuery(`FROM environments WHERE project_id`).WithArgs(s.ProjectID, "staging").WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).AddRow(s.EnvironmentID, s.ProjectID, "staging", "janua-staging", now, now))
	mock.ExpectQuery(`FROM onboarding_registrations WHERE repo_full_name`).WithArgs(januaStageRepository).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_full_name", "webhook_id", "webhook_secret", "argocd_app_name", "onboard_status", "config_snapshot", "error_message", "created_at", "updated_at"}).AddRow(s.RegistrationID, s.ProjectID, januaStageRepository, nil, nil, nil, "completed", []byte(`{}`), nil, now, now))
	mock.ExpectQuery(`FROM services WHERE id`).WithArgs(s.Service.ID).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "git_repo", "app_path", "build_config", "volumes", "auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at", "jobs", "type", "region", "health_check", "k8s_namespace"}).AddRow(s.Service.ID, s.ProjectID, s.Service.Name, s.Service.GitRepo, "apps/api", []byte(`{}`), []byte(`[]`), false, "main", "staging", now, now, []byte(`[]`), "web", "fixture-region", nil, "janua-staging"))
	mock.ExpectQuery(`FROM managed_db_plans WHERE code`).WithArgs("standard-0").WillReturnRows(sqlmock.NewRows([]string{"code", "engine", "display_name", "tier", "storage_gb", "cpu_request", "memory_request", "max_connections", "ha_enabled", "replica_count", "available", "price_cents_month", "created_at", "updated_at"}).AddRow("standard-0", "postgres", "Fixture", "standard", 1, "100m", "256Mi", 25, false, 1, true, 0, now, now))
	mock.ExpectQuery(`FROM database_addons`).WithArgs(s.ProjectID).WillReturnRows(sqlmock.NewRows(januaPlanAddonColumns))
}

func TestJanuaStageAdmissionReceiptNeverPromotesAndRejectsAuthorityDrift(t *testing.T) {
	for _, failure := range []string{"", "signature", "native-admission", "authority-drift"} {
		t.Run(failure, func(t *testing.T) {
			f := newJanuaAdmissionFixture(t)
			s, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
			require.NoError(t, err)
			expectJanuaAdmissionProject(f.mock, s, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
			expectJanuaAdmissionSource(f.mock, s)
			if failure == "" || failure == "authority-drift" {
				expectJanuaAdmissionSource(f.mock, s)
			}
			failAt := 0
			if failure == "native-admission" {
				failAt = 2
			}
			var change func()
			if failure == "authority-drift" {
				change = func() {
					resource := corev1.SchemeGroupVersion.WithResource("endpoints")
					object, err := f.client.Tracker().Get(resource, "default", "kubernetes")
					require.NoError(t, err)
					endpoint := object.(*corev1.Endpoints).DeepCopy()
					endpoint.ResourceVersion = "2"
					err = f.client.Tracker().Update(resource, endpoint, "default")
					require.NoError(t, err)
				}
			}
			count := dryJanuaAdmissionReactors(t, f, failAt, change)
			verified := 0
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", uuid.NewString())
				c.Set("user_roles", []string{"admin"})
				c.Set("user_role", "admin")
				c.Set("is_platform_admin", true)
				c.Next()
			})
			router.POST("/v1/admin/projects/:slug/environments/staging/addon-admission", f.h.RequirePlatformOperator(), func(c *gin.Context) {
				f.h.januaStagingAddonAdmission(c, func(context.Context) error {
					verified++
					if failure == "signature" {
						return errors.New("private verifier output")
					}
					return nil
				})
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, f.path, nil))
			require.Equal(t, 1, verified)
			if failure == "" {
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var receipt januaStageAdmissionReceipt
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
				require.NotNil(t, receipt.DryRunOnly)
				require.True(t, *receipt.DryRunOnly)
				require.NotNil(t, receipt.ApplySupported)
				require.False(t, *receipt.ApplySupported)
				require.Equal(t, 4, receipt.ObjectsAdmitted)
				require.NotEmpty(t, receipt.Gates)
				require.Equal(t, "ADMITTED_DRY_RUN_ONLY", receipt.Classification)
			} else {
				require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			}
			if failure == "signature" {
				require.Zero(t, *count)
			}
			for _, private := range []string{"192.0.2.1", "198.51.100.2", "private upstream fixture", "private verifier output", "production-secret-ref", "production-host-fixture"} {
				require.NotContains(t, response.Body.String(), private)
			}
		})
	}
}
