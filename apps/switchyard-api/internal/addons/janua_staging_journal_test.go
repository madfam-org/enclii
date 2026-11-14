package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func januaOperationFixture() JanuaStagingOperation {
	return JanuaStagingOperation{OperationID: uuid.New(), ActorID: uuid.New(), ProjectID: uuid.New(), EnvironmentID: uuid.New(), RegistrationID: uuid.New(), ServiceID: uuid.New(), AddonID: uuid.New(), PlanSHA256: strings.Repeat("a", 64)}
}

func januaNativeIdentityReactor(kube *fake.Clientset) {
	kube.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		object := action.(ktesting.CreateAction).GetObject()
		meta := object.(metav1.Object)
		meta.SetUID(k8stypes.UID(uuid.NewString()))
		meta.SetResourceVersion("1")
		return false, nil, nil
	})
}

func TestJanuaJournalExactReplayIsReadonlyAndChangedPayloadCannotClaim(t *testing.T) {
	ctx, kube, operation := context.Background(), fake.NewSimpleClientset(), januaOperationFixture()
	januaNativeIdentityReactor(kube)
	created, uid, err := ClaimJanuaStagingOperation(ctx, kube, operation)
	require.NoError(t, err)
	require.True(t, created)
	require.NotEmpty(t, uid)
	kube.ClearActions()
	created, replayUID, err := ClaimJanuaStagingOperation(ctx, kube, operation)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, uid, replayUID)
	for _, action := range kube.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
	for _, change := range []func(*JanuaStagingOperation){
		func(o *JanuaStagingOperation) { o.OperationID = uuid.New() },
		func(o *JanuaStagingOperation) { o.ActorID = uuid.New() },
		func(o *JanuaStagingOperation) { o.EnvironmentID = uuid.New() },
		func(o *JanuaStagingOperation) { o.PlanSHA256 = strings.Repeat("b", 64) },
	} {
		other := operation
		change(&other)
		_, _, err = ClaimJanuaStagingOperation(ctx, kube, other)
		require.Error(t, err)
	}
}

func TestJanuaJournalUnknownCreateOutcomeNeverBecomesExecutionPermission(t *testing.T) {
	kube := fake.NewSimpleClientset()
	kube.PrependReactor("create", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, fmt.Errorf("timeout") })
	created, uid, err := ClaimJanuaStagingOperation(context.Background(), kube, januaOperationFixture())
	require.Error(t, err)
	require.False(t, created)
	require.Empty(t, uid)
}

func TestJanuaBirthReceiptSurvivesNamespaceLossAndRejectsReplacementClaim(t *testing.T) {
	ctx, kube, operation := context.Background(), fake.NewSimpleClientset(), januaOperationFixture()
	januaNativeIdentityReactor(kube)
	_, uid, err := ClaimJanuaStagingOperation(ctx, kube, operation)
	require.NoError(t, err)
	receipt := JanuaStagingBirthReceipt{JanuaStagingNamespaceBinding: JanuaStagingNamespaceBinding{ApplicationUID: "app-birth", AddonUID: "db-birth"}, ClusterUID: "cluster-birth", RuntimeSecretUID: "runtime-birth", RuntimeResourceVersion: "2", PolicyUIDs: []k8stypes.UID{"database-policy", "consumer-policy", "probe-policy"}}
	require.NoError(t, WriteJanuaStagingBirthReceipt(ctx, kube, operation, uid, receipt))
	actual, err := ReadJanuaStagingBirthReceipt(ctx, kube, operation, uid)
	require.NoError(t, err)
	require.Equal(t, receipt, actual)
	cm, err := kube.CoreV1().ConfigMaps("enclii").Get(ctx, operation.journalName()+"-birth", metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, cm.OwnerReferences, "namespace deletion must not garbage-collect control evidence")
	_, err = ReadJanuaStagingBirthReceipt(ctx, kube, operation, "replaced-claim")
	require.Error(t, err)
	// The second create is not an idempotent adoption of the discovered receipt.
	require.Error(t, WriteJanuaStagingBirthReceipt(ctx, kube, operation, uid, receipt))
}

func TestJanuaCustodySerializationCannotExposeRuntimeData(t *testing.T) {
	body, err := json.Marshal(&JanuaStagingCustody{Data: map[string]interface{}{"database_url": "sensitive-fixture", "database_ca": "public-fixture"}, PublicCASHA256: strings.Repeat("a", 64)})
	require.NoError(t, err)
	require.NotContains(t, string(body), "sensitive-fixture")
	require.NotContains(t, string(body), "database_url")
	require.NotContains(t, string(body), "database_ca")
}

func TestJanuaPhaseRetainsOriginalReasonAndRejectsRewrite(t *testing.T) {
	ctx, kube, o := context.Background(), fake.NewSimpleClientset(), januaOperationFixture()
	januaNativeIdentityReactor(kube)
	_, uid, err := ClaimJanuaStagingOperation(ctx, kube, o)
	require.NoError(t, err)
	fresh, err := ClaimJanuaStagingPhase(ctx, kube, o, uid, "reserve", "original reviewed reason")
	require.NoError(t, err)
	require.True(t, fresh)
	kube.ClearActions()
	fresh, err = ClaimJanuaStagingPhase(ctx, kube, o, uid, "reserve", "original reviewed reason")
	require.NoError(t, err)
	require.False(t, fresh)
	_, err = ReadJanuaStagingPhase(ctx, kube, o, uid, "reserve", "attempted reason rewrite")
	require.Error(t, err)
	for _, action := range kube.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
	actual, err := kube.CoreV1().ConfigMaps("enclii").Get(ctx, o.journalName()+"-reserve-claim", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "original reviewed reason", actual.Data["reason"])
}
