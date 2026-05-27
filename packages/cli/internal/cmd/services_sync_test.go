package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestReadRawServiceSpecsSkipsKubernetesServices(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "k8s-service.yaml", `apiVersion: v1
kind: Service
metadata:
  name: cms-backend
spec:
  selector:
    app: cms-backend
`)

	writeTestFile(t, dir, "enclii-service.yaml", `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: switchyard-api
  project: enclii
spec:
  build:
    type: dockerfile
    source:
      git:
        repository: https://github.com/madfam-org/enclii
        branch: main
        autoDeploy: true
`)

	specs, err := readRawServiceSpecs(dir)
	if err != nil {
		t.Fatalf("readRawServiceSpecs returned error: %v", err)
	}

	if len(specs) != 1 {
		t.Fatalf("expected 1 Enclii service spec, got %d", len(specs))
	}
	if specs[0].Metadata.Name != "switchyard-api" {
		t.Fatalf("expected switchyard-api, got %q", specs[0].Metadata.Name)
	}
}

func TestReadRawServiceSpecsReadsMultiDocumentEncliiFiles(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, ".enclii.yml", `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: switchyard-api
  project: enclii
spec:
  build:
    type: dockerfile
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: switchyard-ui
  project: enclii
spec:
  build:
    type: dockerfile
`)

	specs, err := readRawServiceSpecs(dir)
	if err != nil {
		t.Fatalf("readRawServiceSpecs returned error: %v", err)
	}

	if len(specs) != 2 {
		t.Fatalf("expected 2 Enclii service specs, got %d", len(specs))
	}
	if specs[0].Metadata.Name != "switchyard-api" {
		t.Fatalf("expected first spec switchyard-api, got %q", specs[0].Metadata.Name)
	}
	if specs[1].Metadata.Name != "switchyard-ui" {
		t.Fatalf("expected second spec switchyard-ui, got %q", specs[1].Metadata.Name)
	}
}

func TestRawSpecToServiceCarriesBuildSourceMetadata(t *testing.T) {
	spec := &RawServiceSpec{}
	spec.Metadata.Name = "forgesight-app"
	spec.Spec.Build.Type = "dockerfile"
	spec.Spec.Build.Dockerfile = "apps/app/Dockerfile"
	spec.Spec.Build.Context = "."
	spec.Spec.Build.Source.Git.Repository = "https://github.com/madfam-org/forgesight"
	spec.Spec.Build.Source.Git.Path = "apps/app"
	spec.Spec.Build.Source.Git.Branch = "main"
	spec.Spec.Build.Source.Git.AutoDeploy = true

	service := rawSpecToService(spec)

	if service.GitRepo != "https://github.com/madfam-org/forgesight" {
		t.Fatalf("expected ForgeSight git repo, got %q", service.GitRepo)
	}
	if service.AppPath != "apps/app" {
		t.Fatalf("expected apps/app path, got %q", service.AppPath)
	}
	if service.BuildConfig.Type != types.BuildTypeDockerfile {
		t.Fatalf("expected dockerfile build type, got %q", service.BuildConfig.Type)
	}
	if service.BuildConfig.Dockerfile != "apps/app/Dockerfile" {
		t.Fatalf("expected app Dockerfile, got %q", service.BuildConfig.Dockerfile)
	}
	if service.BuildConfig.Context != "." {
		t.Fatalf("expected repo-root context, got %q", service.BuildConfig.Context)
	}
	if !service.AutoDeploy {
		t.Fatal("expected auto deploy to be enabled")
	}
}

func TestRawSpecToServiceMarksRuntimeDisabledServicesBuildOnly(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "blueprint-api.yaml", `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: api
  project: blueprint-harvester
spec:
  build:
    type: dockerfile
    dockerfile: services/api/Dockerfile
    source:
      git:
        repository: https://github.com/madfam-org/blueprint-harvester
        branch: main
        autoDeploy: true
  runtime:
    enabled: false
`)

	specs, err := readRawServiceSpecs(dir)
	if err != nil {
		t.Fatalf("readRawServiceSpecs returned error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 service spec, got %d", len(specs))
	}

	service := rawSpecToService(specs[0])
	if !service.AutoDeploy {
		t.Fatal("expected webhook auto-build to remain enabled")
	}
	if !service.BuildConfig.BuildOnly {
		t.Fatal("expected runtime.enabled=false to persist build_only=true")
	}
}

func TestRawSpecToServiceCarriesBuildArgsAliases(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "dhanam-web.yaml", `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: dhanam-web
  project: dhanam
spec:
  build:
    type: dockerfile
    dockerfile: apps/web/Dockerfile
    context: .
    args:
      NEXT_PUBLIC_API_URL: https://api.dhan.am/v1
      NEXT_PUBLIC_BASE_URL: https://app.dhan.am
    build_args:
      NEXT_PUBLIC_ADMIN_URL: https://admin.dhan.am
    buildArgs:
      NEXT_PUBLIC_BASE_URL: https://override.dhan.am
`)

	specs, err := readRawServiceSpecs(dir)
	if err != nil {
		t.Fatalf("readRawServiceSpecs returned error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 service spec, got %d", len(specs))
	}

	service := rawSpecToService(specs[0])
	if service.BuildConfig.BuildArgs["NEXT_PUBLIC_API_URL"] != "https://api.dhan.am/v1" {
		t.Fatalf("expected args alias to populate build arg, got %q", service.BuildConfig.BuildArgs["NEXT_PUBLIC_API_URL"])
	}
	if service.BuildConfig.BuildArgs["NEXT_PUBLIC_ADMIN_URL"] != "https://admin.dhan.am" {
		t.Fatalf("expected build_args alias to populate build arg, got %q", service.BuildConfig.BuildArgs["NEXT_PUBLIC_ADMIN_URL"])
	}
	if service.BuildConfig.BuildArgs["NEXT_PUBLIC_BASE_URL"] != "https://override.dhan.am" {
		t.Fatalf("expected buildArgs to take precedence, got %q", service.BuildConfig.BuildArgs["NEXT_PUBLIC_BASE_URL"])
	}
}

func TestRawSpecToServiceCarriesServiceJobs(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "tulana-api.yaml", `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: tulana-api
  project: tulana
spec:
  build:
    type: dockerfile
  jobs:
    - name: pull-catalog
      schedule: "20 6 * * *"
      timezone: America/Mexico_City
      command: ["python", "manage.py", "tulana_pull_catalog"]
      timeout: 3600
      retries: 2
`)

	specs, err := readRawServiceSpecs(dir)
	if err != nil {
		t.Fatalf("readRawServiceSpecs returned error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 service spec, got %d", len(specs))
	}

	service := rawSpecToService(specs[0])
	if len(service.Jobs) != 1 {
		t.Fatalf("expected 1 service job, got %d", len(service.Jobs))
	}
	job := service.Jobs[0]
	if job.Name != "pull-catalog" {
		t.Fatalf("expected pull-catalog, got %q", job.Name)
	}
	if job.Schedule != "20 6 * * *" {
		t.Fatalf("expected schedule to be preserved, got %q", job.Schedule)
	}
	if job.Timezone != "America/Mexico_City" {
		t.Fatalf("expected timezone to be preserved, got %q", job.Timezone)
	}
	if len(job.Command) != 3 || job.Command[2] != "tulana_pull_catalog" {
		t.Fatalf("expected command argv to be preserved, got %#v", job.Command)
	}
	if job.Timeout != 3600 || job.Retries != 2 {
		t.Fatalf("expected timeout/retries to be preserved, got timeout=%d retries=%d", job.Timeout, job.Retries)
	}
}

func TestServiceReconcileChangesDetectsPersistedSourceDrift(t *testing.T) {
	existing := &types.Service{
		Name:             "forgesight-app",
		GitRepo:          "",
		AppPath:          "",
		AutoDeploy:       false,
		AutoDeployBranch: "",
		AutoDeployEnv:    "",
		BuildConfig:      types.BuildConfig{Type: types.BuildTypeAuto},
	}
	desired := &types.Service{
		Name:             "forgesight-app",
		GitRepo:          "https://github.com/madfam-org/forgesight",
		AppPath:          "apps/app",
		AutoDeploy:       true,
		AutoDeployBranch: "main",
		AutoDeployEnv:    "production",
		BuildConfig: types.BuildConfig{
			Type:       types.BuildTypeDockerfile,
			Dockerfile: "apps/app/Dockerfile",
			Context:    ".",
		},
	}

	changes := serviceReconcileChanges(existing, desired)
	want := []string{"git_repo", "app_path", "auto_deploy", "auto_deploy_branch", "auto_deploy_env", "build_config"}
	if len(changes) != len(want) {
		t.Fatalf("expected %d changes, got %d: %v", len(want), len(changes), changes)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("change[%d] = %q, want %q", i, changes[i], want[i])
		}
	}
}

func TestServiceReconcileChangesDetectsJobDrift(t *testing.T) {
	existing := &types.Service{
		Name:        "tulana-api",
		GitRepo:     "https://github.com/madfam-org/tulana",
		BuildConfig: types.BuildConfig{Type: types.BuildTypeDockerfile},
	}
	desired := &types.Service{
		Name:        "tulana-api",
		GitRepo:     "https://github.com/madfam-org/tulana",
		BuildConfig: types.BuildConfig{Type: types.BuildTypeDockerfile},
		Jobs: []types.JobSpec{{
			Name:     "pull-catalog",
			Schedule: "20 6 * * *",
			Command:  []string{"python", "manage.py", "tulana_pull_catalog"},
		}},
	}

	changes := serviceReconcileChanges(existing, desired)
	if len(changes) != 1 || changes[0] != "jobs" {
		t.Fatalf("expected only jobs drift, got %v", changes)
	}
}

func TestServiceReconcileChangesTreatsNilAndEmptyJobsAsAligned(t *testing.T) {
	existing := &types.Service{
		Name:        "tulana-web",
		GitRepo:     "https://github.com/madfam-org/tulana",
		BuildConfig: types.BuildConfig{Type: types.BuildTypeDockerfile},
		Jobs:        []types.JobSpec{},
	}
	desired := &types.Service{
		Name:        "tulana-web",
		GitRepo:     "https://github.com/madfam-org/tulana",
		BuildConfig: types.BuildConfig{Type: types.BuildTypeDockerfile},
		Jobs:        nil,
	}

	if changes := serviceReconcileChanges(existing, desired); len(changes) != 0 {
		t.Fatalf("expected no drift for empty jobs, got %v", changes)
	}
}

func TestServiceReconcileChangesReturnsEmptyWhenAligned(t *testing.T) {
	service := &types.Service{
		Name:             "forgesight-app",
		GitRepo:          "https://github.com/madfam-org/forgesight",
		AppPath:          "apps/app",
		AutoDeploy:       true,
		AutoDeployBranch: "main",
		AutoDeployEnv:    "production",
		BuildConfig: types.BuildConfig{
			Type:       types.BuildTypeDockerfile,
			Dockerfile: "apps/app/Dockerfile",
			Context:    ".",
		},
	}

	if changes := serviceReconcileChanges(service, service); len(changes) != 0 {
		t.Fatalf("expected no reconcile changes, got %v", changes)
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
