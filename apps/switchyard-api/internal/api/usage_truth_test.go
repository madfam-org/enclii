package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// RFC 0049 Phase 1 truth pass: /v1/usage shows measured numbers or says that
// nothing was measured. No invented estimate, rate or allowance.

func TestNotMeasuredMetric_IsFlaggedUnpricedAndHasNoAllowance(t *testing.T) {
	m := notMeasuredMetric("storage", "Storage", "GB", storageNotMeasuredNote)

	assert.True(t, m.Unavailable)
	assert.Equal(t, storageNotMeasuredNote, m.Note)
	assert.Zero(t, m.Used)
	assert.Zero(t, m.Cost)
	assert.Equal(t, -1.0, m.Included)

	raw, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"unavailable":true`)
}

func TestCostBreakdown_ReportsNothingPriced(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	got, err := (&Handler{}).calculateCostBreakdown(context.Background(), start, start.AddDate(0, 1, 0).Add(-time.Second))
	require.NoError(t, err)

	assert.False(t, got.Priced)
	assert.Equal(t, usagePricingNote, got.PricingNote)
	assert.Empty(t, got.Categories)
	assert.Zero(t, got.TotalUsage)
	assert.Equal(t, "2026-11-01", got.PeriodStart)
	assert.Equal(t, "2026-11-30", got.PeriodEnd)
}

func svcFixture(name string) *types.Service {
	return &types.Service{ID: uuid.New(), Name: name}
}

func clusterFixture(pods ...k8s.PodMetrics) *k8s.ClusterMetrics {
	cm := &k8s.ClusterMetrics{MetricsEnabled: true, Namespaces: map[string]*k8s.NamespaceMetrics{}}
	for _, pm := range pods {
		ns, ok := cm.Namespaces[pm.Namespace]
		if !ok {
			ns = &k8s.NamespaceMetrics{Namespace: pm.Namespace}
			cm.Namespaces[pm.Namespace] = ns
		}
		ns.Pods = append(ns.Pods, pm)
		cm.TotalCPU += pm.TotalCPU
		cm.TotalMemory += pm.TotalMemory
		cm.TotalPods++
	}
	return cm
}

func TestAttributeServiceMetrics_SumsOnlyTheServicesOwnPods(t *testing.T) {
	api, web, idle := svcFixture("api"), svcFixture("web"), svcFixture("idle")
	nsOf := map[uuid.UUID]string{api.ID: "shop", web.ID: "shop", idle.ID: "shop"}
	podApps := map[string]map[string]string{
		"shop": {"api-1": "api", "api-2": "api", "web-1": "web", "redis-0": "redis"},
	}
	cluster := clusterFixture(
		k8s.PodMetrics{PodName: "api-1", Namespace: "shop", TotalCPU: 100, TotalMemory: 64 << 20},
		k8s.PodMetrics{PodName: "api-2", Namespace: "shop", TotalCPU: 50, TotalMemory: 32 << 20},
		k8s.PodMetrics{PodName: "web-1", Namespace: "shop", TotalCPU: 10, TotalMemory: 16 << 20},
		k8s.PodMetrics{PodName: "redis-0", Namespace: "shop", TotalCPU: 5, TotalMemory: 8 << 20},
	)

	got := attributeServiceMetrics([]*types.Service{api, web, idle}, nsOf, podApps, cluster)
	require.Len(t, got, 3)

	assert.Equal(t, "running", got[0].Status)
	assert.Equal(t, 2, got[0].PodCount)
	assert.InDelta(t, 150, got[0].CPUUsage, 1e-9)
	assert.InDelta(t, 96, got[0].MemoryUsage, 1e-9)

	assert.Equal(t, "running", got[1].Status)
	assert.InDelta(t, 10, got[1].CPUUsage, 1e-9)

	// Listed namespace, no pod with its label: stopped, a real zero.
	assert.Equal(t, "stopped", got[2].Status)
	assert.Zero(t, got[2].PodCount)
}

func TestAttributeServiceMetrics_UnreadableIsUnknownNotZero(t *testing.T) {
	a := svcFixture("a")
	nsOf := map[uuid.UUID]string{a.ID: "ns-a"}

	// metrics-server unreadable
	got := attributeServiceMetrics([]*types.Service{a}, nsOf, map[string]map[string]string{"ns-a": {}}, &k8s.ClusterMetrics{MetricsEnabled: false})
	assert.Equal(t, "unknown", got[0].Status)

	// pod list for the namespace failed
	got = attributeServiceMetrics([]*types.Service{a}, nsOf, map[string]map[string]string{}, clusterFixture())
	assert.Equal(t, "unknown", got[0].Status)

	// no metrics at all
	got = attributeServiceMetrics([]*types.Service{a}, nsOf, nil, nil)
	assert.Equal(t, "unknown", got[0].Status)
}
