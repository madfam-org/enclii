package logstream

import (
	"context"

	"github.com/google/uuid"
)

// ServiceResolver abstracts the switchyard DB lookups the handler needs.
// Decoupling via an interface keeps this package testable without
// pulling in the full `db` package + sqlmock + migrations.
//
// Production wiring lives at cmd/api/main.go; tests stub with a
// hand-rolled struct.
type ServiceResolver interface {
	// Resolve looks up the service + enclosing project/env and returns
	// the bits the handler needs to build a LogQL selector. Returning
	// (nil, nil) MUST be treated as "not found"; any error is an
	// infrastructure failure.
	Resolve(ctx context.Context, serviceID uuid.UUID, envName string) (*ServiceCoords, error)
}

// ServiceCoords is the resolved identity of a deployed service. These
// are the labels Loki sees because of Fluent Bit's k8s metadata
// enrichment — changing this struct means also changing the LogQL
// selector in logql.go.
type ServiceCoords struct {
	ServiceID   uuid.UUID
	ServiceName string
	ProjectID   uuid.UUID
	ProjectSlug string
	Environment string // resolved env name (e.g., "production")
	Namespace   string // derived: "enclii-<projectSlug>-<env>"
}

// Authz abstracts the project-membership check. Matches the pattern in
// internal/audit.AuthzChecker — we keep it separate because the checks
// are orthogonal (audit is admin-or-self, logs is project-member).
type Authz interface {
	// CanReadService returns true if the authenticated caller on the
	// gin.Context may read logs for the given service. Implementation
	// reads project membership from the switchyard auth tables.
	CanReadService(ctx context.Context, callerSub string, coords *ServiceCoords) (bool, error)
	// CallerSub returns the caller's user_id / sub. Empty string for
	// unauthenticated requests (which should never reach us — the auth
	// middleware 401s earlier).
	CallerSub(ctx context.Context) string
}
