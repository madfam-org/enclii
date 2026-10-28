package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func fenceReadRequestBody() string {
	return `{"profile":"portal-sim-reset-fence-read","service_id":"11000000-0000-4000-8000-000000000001","pedido":{"version":"crea-sim-fence-kernel-read-v1","nucleoId":"crea-sim-map01"},"dry_run":true,"idempotency_key":"11000000-0000-4000-8000-000000000003"}`
}

func TestPortalFenceReadRequestIsClosedBeforeCustodyAccess(t *testing.T) {
	valid := fenceReadRequestBody()
	r, err := parsePortalFenceRead([]byte(valid))
	require.NoError(t, err)
	require.True(t, *r.DryRun)
	for _, body := range []string{
		strings.Replace(valid, `"dry_run":true,`, "", 1), strings.Replace(valid, `"dry_run":true`, `"dry_run":false`, 1),
		strings.Replace(valid, `"profile":`, `"command":["node","-e","prohibited"],"profile":`, 1),
		strings.Replace(valid, `"profile":`, `"image":"foreign","profile":`, 1), strings.Replace(valid, `"profile":`, `"uri":"prohibited","profile":`, 1),
		strings.Replace(valid, `"version":`, `"sql":"SELECT 1","version":`, 1), strings.Replace(valid, `"nucleoId":"crea-sim-map01"`, `"nucleoId":"crea-sim-map01","nucleo\u0049d":"crea-sim-map02"`, 1),
		strings.Replace(valid, "crea-sim-map01", "unowned", 1), strings.Replace(valid, "portal-sim-reset-fence-read", "portal-sim-initial", 1), valid + "{}", string([]byte{255}), strings.Repeat(" ", 8193),
	} {
		_, err := parsePortalFenceRead([]byte(body))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "prohibited")
	}
	apply := strings.Replace(valid, `"dry_run":true`, `"dry_run":false,"reason":"read-only observation","expect_plan_sha256":"`+strings.Repeat("a", 64)+`"`, 1)
	r, err = parsePortalFenceRead([]byte(apply))
	require.NoError(t, err)
	require.False(t, *r.DryRun)
}

func TestPortalFenceReadProfilePinsExactReaderAndCannotUseInitialOwnerRoute(t *testing.T) {
	p, err := isolatedBootstrapProfileFor("portal-sim-reset-fence-read")
	require.NoError(t, err)
	require.Equal(t, []bootstrapSourceRequirement{{Path: "packages/entrenamiento/src/leer-freno-reset.ts", SHA256: "ee9aea0ecb93847ffef4b1f544c62f157c3356b9d34ac5d0b0e007a3712b07c7"}, {Path: "packages/entrenamiento/sql/002-custodia-reset.sql", SHA256: "66b3ab6bc4306efcb75ee75cdad85084ac1c6b9a6fd4542df99c0a7aafdf8496"}, {Path: "packages/entrenamiento/sql/004-cierre-por-reconstruccion.sql", SHA256: "a9ea106ec748408ec39bced9c50686c06552ca58ee0eb1fbf154b598e41227e3"}, {Path: "packages/entrenamiento/src/cerrar-por-reconstruccion.ts", SHA256: "8863e97b89f889ec88f329c1c4994d8770ea575429569f1c81d9eb49dccc0b76"}}, p.SourceRequirements)
	require.True(t, p.portalDatabase())
	require.False(t, k8s.IsFixedStepProfile(p.Name))
	require.True(t, k8s.IsRepeatedProfile(p.Name))
	require.Equal(t, "crea_sim_portal", p.RuntimeRole)
	require.Equal(t, "crea-sim-gateway", p.Service)
}

func TestPortalFenceReadProducerRefusesMissingOrDifferentReaderBeforeAnySignature(t *testing.T) {
	for _, scenario := range []string{"qualified", "missing reader", "different reader", "missing custody", "no source reader"} {
		t.Run(scenario, func(t *testing.T) {
			p, err := isolatedBootstrapProfileFor("portal-sim-reset-fence-read")
			require.NoError(t, err)
			// The producer gate is exercised with a synthetic body and independent hash.
			reader, schema := "// synthetic observation only\n", "-- synthetic custody contract\n"
			p.SourceRequirements[0].SHA256 = strings.TrimPrefix(bootstrapDigestOf(reader), "sha256:")
			p.SourceRequirements[1].SHA256 = strings.TrimPrefix(bootstrapDigestOf(schema), "sha256:")
			p.SourceRequirements[2].SHA256 = strings.TrimPrefix(bootstrapDigestOf(schema), "sha256:")
			p.SourceRequirements[3].SHA256 = strings.TrimPrefix(bootstrapDigestOf(reader), "sha256:")
			a, image, revision, signatures := fixedStepProducerChain(t, p)
			if scenario != "no source reader" {
				a.ReadSource = func(_ context.Context, _ isolatedBootstrapProfile, path, at string) (string, error) {
					require.Equal(t, revision, at)
					if strings.HasSuffix(path, ".ts") {
						if scenario == "missing reader" {
							return "", errors.New("unavailable")
						}
						if scenario == "different reader" {
							return "prohibited", nil
						}
						return reader, nil
					}
					if scenario == "missing custody" {
						return "", errors.New("unavailable")
					}
					return schema, nil
				}
			}
			proof, err := a.verify(t.Context(), p, image)
			if scenario == "qualified" {
				require.NoError(t, err)
				require.Equal(t, 4, len(proof.VerifiedSources))
				require.Equal(t, 1, *signatures)
			} else {
				require.Error(t, err)
				require.Nil(t, proof)
				require.Zero(t, *signatures)
				require.NotContains(t, err.Error(), "prohibited")
			}
		})
	}
}

func TestPortalFenceReadInputDoesNotEncodeAnyIdentityOrCredentials(t *testing.T) {
	r, err := parsePortalFenceRead([]byte(fenceReadRequestBody()))
	require.NoError(t, err)
	b, err := json.Marshal(r.Input)
	require.NoError(t, err)
	require.NotContains(t, string(b), "sub")
	require.NotContains(t, string(b), "secret")
}

func TestPortalFenceReadRequiresReadyConvergedRuntimeWithoutScalingIt(t *testing.T) {
	p, err := isolatedBootstrapProfileFor("portal-sim-reset-fence-read")
	require.NoError(t, err)
	project := uuid.New()
	for _, scenario := range []string{"ready", "zero replicas", "not Ready", "not available", "old Pods remain"} {
		t.Run(scenario, func(t *testing.T) {
			d := fixedStepDeployment(p.Service, 1)
			d.Status.AvailableReplicas = 1
			switch scenario {
			case "zero replicas":
				d = fixedStepDeployment(p.Service, 0)
			case "not Ready":
				d.Status.ReadyReplicas = 0
			case "not available":
				d.Status.AvailableReplicas = 0
			case "old Pods remain":
				d.Status.Replicas = 2
			}
			client := kubefake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace, UID: "namespace-fixture", Labels: map[string]string{k8s.LabelProjectNamespace: project.String()}}}, d)
			h := &Handler{k8sClient: &k8s.Client{KubeClient: client}}
			_, err := h.initialWorkload(t.Context(), p, project)
			require.Equal(t, scenario == "ready", err == nil)
			for _, a := range client.Actions() {
				require.Equal(t, "get", a.GetVerb())
			}
		})
	}
}
