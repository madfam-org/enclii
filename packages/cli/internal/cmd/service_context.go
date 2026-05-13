package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type localServiceContext struct {
	Name    string
	Project string
}

type resolvedServiceRef struct {
	ID      string
	Name    string
	Project string
}

func resolveOperationalService(ctx context.Context, apiClient *client.APIClient, cfg *config.Config, serviceRef, specFile string) (*resolvedServiceRef, error) {
	if _, err := uuid.Parse(serviceRef); err == nil {
		return &resolvedServiceRef{ID: serviceRef, Name: serviceRef, Project: projectFromConfig(cfg)}, nil
	}

	localServices := readLocalServiceContexts(specFile)
	projectSlug := projectFromConfig(cfg)
	if projectSlug == "default" {
		if project := projectForService(localServices, serviceRef); project != "" {
			projectSlug = project
		}
	}

	serviceName := serviceRef
	if serviceName == "" {
		if len(localServices) == 0 {
			return nil, fmt.Errorf("service name or service ID required (no service.yaml or .enclii.yml found in cwd)")
		}
		serviceName = localServices[0].Name
		if localServices[0].Project != "" && (cfg.Project == "" || cfg.Project == "default") {
			projectSlug = localServices[0].Project
		}
	}

	services, err := apiClient.ListServices(ctx, projectSlug)
	if err != nil {
		return nil, fmt.Errorf("list services in project %q: %w", projectSlug, err)
	}
	for _, svc := range services {
		if svc.Name == serviceName {
			return &resolvedServiceRef{
				ID:      svc.ID.String(),
				Name:    svc.Name,
				Project: projectSlug,
			}, nil
		}
	}

	return nil, fmt.Errorf("service %q not found in project %q", serviceName, projectSlug)
}

func projectFromConfig(cfg *config.Config) string {
	if cfg == nil || cfg.Project == "" {
		return "default"
	}
	return cfg.Project
}

func projectForService(services []localServiceContext, serviceName string) string {
	if len(services) == 0 {
		return ""
	}
	if serviceName == "" {
		return services[0].Project
	}
	for _, svc := range services {
		if svc.Name == serviceName {
			return svc.Project
		}
	}
	return services[0].Project
}

func readLocalServiceContexts(specFile string) []localServiceContext {
	seen := map[string]bool{}
	var services []localServiceContext
	for _, path := range localServiceContextFiles(specFile) {
		for _, svc := range parseLocalServiceContexts(path) {
			key := svc.Project + "/" + svc.Name
			if svc.Name == "" || seen[key] {
				continue
			}
			seen[key] = true
			services = append(services, svc)
		}
	}
	return services
}

func localServiceContextFiles(specFile string) []string {
	candidates := []string{}
	if strings.TrimSpace(specFile) != "" {
		candidates = append(candidates, specFile)
	}
	for _, path := range []string{"service.yaml", ".enclii.yml"} {
		already := false
		for _, candidate := range candidates {
			if candidate == path {
				already = true
				break
			}
		}
		if !already {
			candidates = append(candidates, path)
		}
	}
	return candidates
}

func parseLocalServiceContexts(path string) []localServiceContext {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	decoder := yaml.NewDecoder(f)
	var services []localServiceContext
	for {
		var doc struct {
			Kind     string                `yaml:"kind"`
			Metadata types.ServiceMetadata `yaml:"metadata"`
		}
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		if doc.Kind != "" && doc.Kind != "Service" {
			continue
		}
		if doc.Metadata.Name == "" {
			continue
		}
		services = append(services, localServiceContext{
			Name:    doc.Metadata.Name,
			Project: doc.Metadata.Project,
		})
	}
	return services
}
