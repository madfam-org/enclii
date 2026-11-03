package januastage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func authorityFixture() Authority {
	project, intent := uuid.New(), uuid.New()
	return Authority{ID: uuid.New(), ProjectID: project, EnvironmentID: uuid.New(),
		RepositoryRegistrationID: uuid.New(), HeldIntentID: intent,
		HeldServiceID: HeldServiceID(project, intent), Profile: Profile,
		NamespaceUID: "ns-fixture", DeploymentUID: "api-fixture", APIServiceUID: "api-service-fixture",
		RedisDeploymentUID: "redis-fixture", RedisServiceUID: "redis-service-fixture",
		ApplicationUID: "application-fixture", AppProjectUID: "project-fixture",
		SourceRevision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64),
		PlanSHA256: strings.Repeat("c", 64), ActivationHeld: true,
		ObservedAt: time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)}
}

func TestHeldAuthorityShapeNeverClaimsRuntimeActivation(t *testing.T) {
	a := authorityFixture()
	require.NoError(t, ValidateAuthorityShape(a))
	for _, mutate := range []func(*Authority){
		func(a *Authority) { a.HeldServiceID = uuid.New() },
		func(a *Authority) { a.EnvironmentID = uuid.Nil },
		func(a *Authority) { a.Profile = "production" },
		func(a *Authority) { a.ActivationHeld = false },
		func(a *Authority) { a.ActiveServiceRegistered = true },
		func(a *Authority) { a.Native043BindingProven = true },
		func(a *Authority) { a.SourceRevision = "main" },
		func(a *Authority) { a.SourceSHA256 = "" },
		func(a *Authority) { a.NamespaceUID = "" },
		func(a *Authority) { a.DeploymentUID = a.ApplicationUID },
		func(a *Authority) { a.ObservedAt = "yesterday" },
	} {
		changed := a
		mutate(&changed)
		require.ErrorIs(t, ValidateAuthorityShape(changed), ErrRefused)
	}
}
