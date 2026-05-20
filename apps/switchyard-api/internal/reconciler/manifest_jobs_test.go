package reconciler

import "testing"

func TestServiceManagedCronJobNameUsesProjectPrefixForRoleServices(t *testing.T) {
	got := serviceManagedCronJobName("tulana", "tulana-api", "pull-catalog")
	if got != "tulana-pull-catalog" {
		t.Fatalf("expected tulana-pull-catalog, got %q", got)
	}

	got = serviceManagedCronJobName("tulana", "tulana-api", "tulana-pull-catalog")
	if got != "tulana-pull-catalog" {
		t.Fatalf("expected pre-prefixed name to be preserved, got %q", got)
	}

	got = serviceManagedCronJobName("platform", "worker", "refresh")
	if got != "worker-refresh" {
		t.Fatalf("expected worker-refresh, got %q", got)
	}
}
