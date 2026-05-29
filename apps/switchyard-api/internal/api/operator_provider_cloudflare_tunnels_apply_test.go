package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

func TestPlanTunnelRouteDrifts(t *testing.T) {
	live := []services.IngressRule{
		{Hostname: "app.example.com", Service: "http://wrong.ns.svc.cluster.local:80"},
		{Hostname: "api.example.com", Service: "http://api.prod.svc.cluster.local:80"},
	}
	specs := []*services.RouteSpec{
		{
			Hostname:         "app.example.com",
			ServiceName:      "web",
			ServiceNamespace: "prod",
			ServicePort:      80,
		},
		{
			Hostname:         "api.example.com",
			ServiceName:      "api",
			ServiceNamespace: "prod",
			ServicePort:      80,
		},
		{
			Hostname:         "new.example.com",
			ServiceName:      "web",
			ServiceNamespace: "prod",
			ServicePort:      80,
		},
	}

	plan := planTunnelRouteDrifts(live, specs)
	byHost := map[string]tunnelRoutePlanItem{}
	for _, item := range plan {
		byHost[item.Hostname] = item
	}

	assert.Equal(t, "update", byHost["app.example.com"].Action)
	assert.Equal(t, "http://wrong.ns.svc.cluster.local:80", byHost["app.example.com"].CurrentService)
	assert.Equal(t, "http://web.prod.svc.cluster.local:80", byHost["app.example.com"].DesiredService)

	assert.Equal(t, "skip", byHost["api.example.com"].Action)

	assert.Equal(t, "create", byHost["new.example.com"].Action)
	assert.Equal(t, "", byHost["new.example.com"].CurrentService)
}
