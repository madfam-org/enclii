package addons

import (
	"context"
	"errors"
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestReadPostgresHibernationStateReadsTheAnnotationAndCondition(t *testing.T) {
	dyn, _ := hibClients(t, hibClusterIn(map[string]string{CNPGHibernationAnnotation: "on"}, cnpgHealthyPhase, true), nil)
	s, err := ReadPostgresHibernationState(context.Background(), dyn, hibAddon())
	require.NoError(t, err)
	require.Equal(t, HibernationState{Namespace: hibNS, Cluster: "pg-kernel09b-fixture", ClusterUID: "uid-cluster", Annotated: true, Confirmed: true}, *s)

	dyn, _ = hibClients(t, hibCluster(nil), nil)
	s, err = ReadPostgresHibernationState(context.Background(), dyn, hibAddon())
	require.NoError(t, err)
	require.False(t, s.Annotated, "a running database is not retired")
	require.False(t, s.Confirmed)
}

func TestReadPostgresHibernationStateRefusesWhatTheHoldCannotKeep(t *testing.T) {
	dyn, _ := hibClients(t, hibCluster(nil), nil)
	noCluster := hibAddon()
	noCluster.K8sResourceName = ""
	redis := hibAddon()
	redis.Type = types.DatabaseAddonTypeRedis
	for _, a := range []*types.DatabaseAddon{nil, noCluster, redis} {
		_, err := ReadPostgresHibernationState(context.Background(), dyn, a)
		require.True(t, errors.Is(err, ErrHibernationUnsupported))
	}
	_, err := ReadPostgresHibernationState(context.Background(), nil, hibAddon())
	require.Error(t, err)

	require.True(t, SupportsRetentionHold(hibAddon()))
	shared := hibAddon()
	shared.Plan = "shared-discovered"
	require.False(t, SupportsRetentionHold(shared))
	require.False(t, SupportsRetentionHold(noCluster))
}
