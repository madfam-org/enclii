package addons

import (
	"context"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func karafielStageFixture() *types.DatabaseAddon {
	project, environment := uuid.New(), uuid.New()
	return &types.DatabaseAddon{ID: uuid.New(), ProjectID: project, EnvironmentID: &environment, Name: KarafielStagingAddon, Type: types.DatabaseAddonTypePostgres, Plan: "standard-0", Config: types.DatabaseAddonConfig{DatabaseName: KarafielStagingDatabase, RuntimeRole: KarafielStagingRuntime, CPU: "100m", Memory: "256Mi", StorageGB: 1, Replicas: 1}}
}

func TestKarafielStagingProfileKeepsGenericSyntheticGuardAndOwnerSeparation(t *testing.T) {
	a := karafielStageFixture()
	require.NoError(t, ValidatePostgresRuntimeConfig(a.Type, a.Config))
	require.True(t, KarafielStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID))
	for _, mutation := range []func(*types.DatabaseAddon){
		func(a *types.DatabaseAddon) { a.Config.DatabaseName = "karafiel_staging" },
		func(a *types.DatabaseAddon) { a.Config.RuntimeRole = "enclii_migrator" },
		func(a *types.DatabaseAddon) { a.Name = "other" },
		func(a *types.DatabaseAddon) { a.EnvironmentID = nil },
		func(a *types.DatabaseAddon) { a.Plan = "shared-discovered" },
	} {
		other := karafielStageFixture()
		mutation(other)
		if other.EnvironmentID == nil {
			require.False(t, KarafielStagingAddonOwned(other, a.ProjectID, *a.EnvironmentID))
			continue
		}
		require.False(t, KarafielStagingAddonOwned(other, other.ProjectID, *other.EnvironmentID))
	}
}

func TestKarafielStageManifestUsesNativeRoleBootstrapWithoutBackupProviderOrMutableTag(t *testing.T) {
	t.Setenv("ENCLII_ADDON_BACKUP_DESTINATION_BASE", "s3://fixture-never-import")
	a := karafielStageFixture()
	p := &PostgresProvisioner{}
	c, err := p.KarafielStagingCluster(a)
	require.NoError(t, err)
	image, _, _ := unstructured.NestedString(c.Object, "spec", "imageName")
	require.Equal(t, types.JanuaStagingPostgresImage, image)
	owner, _, _ := unstructured.NestedString(c.Object, "spec", "bootstrap", "initdb", "owner")
	require.Equal(t, "enclii_migrator", owner)
	_, found, _ := unstructured.NestedFieldNoCopy(c.Object, "spec", "backup")
	require.False(t, found)
	superuser, found, _ := unstructured.NestedBool(c.Object, "spec", "enableSuperuserAccess")
	require.True(t, found)
	require.False(t, superuser)
	role, _, _ := unstructured.NestedSlice(c.Object, "spec", "managed", "roles")
	require.Len(t, role, 1)
	require.Equal(t, KarafielStagingRuntime, role[0].(map[string]interface{})["name"])
}

func TestKarafielCatalogQualificationNeverConvertsMetadataFailureOrPopulatedDBIntoEmptyProof(t *testing.T) {
	c := KarafielStagingCatalog{Database: KarafielStagingDatabase, RuntimeRole: KarafielStagingRuntime, Nonowner: true, Unprivileged: true, NoMemberships: true, NoSchemaCreate: true}
	require.True(t, c.Qualifies(true))
	for _, mutation := range []func(*KarafielStagingCatalog){
		func(c *KarafielStagingCatalog) { c.Database = "production" }, func(c *KarafielStagingCatalog) { c.RuntimeRole = "enclii_migrator" },
		func(c *KarafielStagingCatalog) { c.Nonowner = false }, func(c *KarafielStagingCatalog) { c.Unprivileged = false },
		func(c *KarafielStagingCatalog) { c.NoMemberships = false }, func(c *KarafielStagingCatalog) { c.NoSchemaCreate = false },
		func(c *KarafielStagingCatalog) { c.UserTables = 1 }, func(c *KarafielStagingCatalog) { c.Migrations = 1 }, func(c *KarafielStagingCatalog) { c.UserTables = -1 },
	} {
		other := c
		mutation(&other)
		require.False(t, other.Qualifies(true))
	}
}

func TestKarafielClosedClusterRejectsCloneRecoverySQLAndPodLabelInjection(t *testing.T) {
	p := &PostgresProvisioner{}
	expected, err := p.KarafielStagingCluster(karafielStageFixture())
	require.NoError(t, err)
	require.True(t, closedKarafielCluster(expected, expected.DeepCopy()))
	for _, mutation := range []func(map[string]interface{}){
		func(s map[string]interface{}) {
			s["externalClusters"] = []interface{}{map[string]interface{}{"name": "foreign"}}
		},
		func(s map[string]interface{}) {
			s["bootstrap"].(map[string]interface{})["recovery"] = map[string]interface{}{"source": "foreign"}
		},
		func(s map[string]interface{}) {
			s["bootstrap"].(map[string]interface{})["initdb"].(map[string]interface{})["import"] = map[string]interface{}{"type": "microservice"}
		},
		func(s map[string]interface{}) {
			s["inheritedMetadata"] = map[string]interface{}{"labels": map[string]interface{}{"enclii.dev/bootstrap-job": "foreign"}}
		},
		func(s map[string]interface{}) { s["env"] = []interface{}{map[string]interface{}{"name": "foreign"}} },
		func(s map[string]interface{}) {
			s["storage"].(map[string]interface{})["pvcTemplate"] = map[string]interface{}{"volumeName": "foreign-volume"}
		},
		func(s map[string]interface{}) {
			s["postgresql"] = map[string]interface{}{"pg_hba": []interface{}{"host all all all trust"}}
		},
		func(s map[string]interface{}) {
			s["managed"].(map[string]interface{})["roles"].([]interface{})[0].(map[string]interface{})["inRoles"] = []interface{}{"enclii_migrator"}
		},
	} {
		actual := expected.DeepCopy()
		mutation(actual.Object["spec"].(map[string]interface{}))
		require.False(t, closedKarafielCluster(expected, actual))
	}
}

func TestKarafielNativeNetworkReadbackRejectsAdditiveForeignAllows(t *testing.T) {
	a := karafielStageFixture()
	expected, err := KarafielStagingPolicies(a, []KarafielControlPeer{{HostRoute: "10.0.0.1/32", Port: 6443}}, metav1.LabelSelector{MatchLabels: map[string]string{"app": "cnpg-controller-manager"}})
	require.NoError(t, err)
	for _, p := range expected {
		p.UID = k8stypes.UID("fixture-" + p.Name)
		p.ResourceVersion = "1"
	}
	for _, mode := range []string{"exact", "broad", "foreign-cluster", "dns-wildcard"} {
		t.Run(mode, func(t *testing.T) {
			objects := []runtime.Object{expected[0].DeepCopy(), expected[1].DeepCopy()}
			if mode != "exact" {
				p := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: expected[0].Namespace, UID: "foreign-uid", ResourceVersion: "1"}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{}}}}
				if mode == "foreign-cluster" {
					p.Spec.PodSelector.MatchLabels = map[string]string{"cnpg.io/cluster": "another-concrete-cluster"}
				}
				if mode == "dns-wildcard" {
					port := intstr.FromInt32(53)
					p.Spec.Egress[0].Ports = []networkingv1.NetworkPolicyPort{{Port: &port}}
				}
				objects = append(objects, p)
			}
			client := fake.NewSimpleClientset(objects...)
			s := &AddonService{k8sClient: &k8s.Client{KubeClient: client}}
			err := s.VerifyKarafielStagingPolicies(context.Background(), a, expected, true)
			if mode == "exact" || mode == "foreign-cluster" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			for _, action := range client.Actions() {
				require.Contains(t, []string{"get", "list"}, action.GetVerb())
			}
		})
	}
}
