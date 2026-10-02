//go:build integration

package db

// ServiceRepository.GetByID reads services.k8s_namespace, against a real
// Postgres.
//
// Run with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/db/...

import (
	"context"
	"testing"
)

func TestServiceGetByIDReturnsRecordedNamespace(t *testing.T) {
	conn, _ := freshDatabase(t)
	if err := migrateTo(t, conn, 41); err != nil {
		t.Fatalf("migrate to 41: %v", err)
	}
	_, _, serviceID := seedService(t, conn, "nsread")
	repo := NewServiceRepository(conn)

	got, err := repo.GetByID(serviceID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.K8sNamespace != nil {
		t.Fatalf("a service with no recorded namespace must read back nil, got %q", *got.K8sNamespace)
	}

	if err := repo.UpdateK8sNamespace(context.Background(), serviceID, "nsread-adopted"); err != nil {
		t.Fatalf("UpdateK8sNamespace: %v", err)
	}
	got, err = repo.GetByID(serviceID)
	if err != nil {
		t.Fatalf("GetByID after recording a namespace: %v", err)
	}
	if got.K8sNamespace == nil || *got.K8sNamespace != "nsread-adopted" {
		t.Fatalf("GetByID must return the recorded k8s_namespace, got %v", got.K8sNamespace)
	}
}
