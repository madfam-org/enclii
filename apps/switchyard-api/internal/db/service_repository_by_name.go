package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ErrAmbiguousServiceName means more than one project registers a service
// under the name being resolved. Service names are unique per project
// (services_project_id_name_key), not platform-wide, so a bare name is only an
// identity when exactly one project uses it. Callers must narrow by project
// (GetByProjectAndName) or by another ownership fact instead of picking a row.
var ErrAmbiguousServiceName = errors.New("service name is registered in more than one project")

const serviceByNameSelect = `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, k8s_namespace, created_at, updated_at, COALESCE(jobs, '[]'::jsonb) as jobs, type, region
		FROM services`

// GetByName resolves a service by name alone. It succeeds only when the name
// is unique across the platform: no row is sql.ErrNoRows, and two or more rows
// (the same name in different projects) is ErrAmbiguousServiceName, never an
// arbitrary pick. Prefer GetByProjectAndName whenever the project is known.
func (r *ServiceRepository) GetByName(name string) (*types.Service, error) {
	matches, err := r.listByName(name, 2)
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return nil, sql.ErrNoRows
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrAmbiguousServiceName, name)
	}
}

// ListByName returns every service with this name, one per project at most,
// oldest first. Callers narrow the result by an ownership fact (project,
// namespace, git repo) and treat more than one survivor as ambiguous.
func (r *ServiceRepository) ListByName(name string) ([]*types.Service, error) {
	return r.listByName(name, 0)
}

// GetByProjectAndName resolves a service inside one project. (project_id,
// name) is unique, so this is an identity.
func (r *ServiceRepository) GetByProjectAndName(projectID uuid.UUID, name string) (*types.Service, error) {
	row := r.db.QueryRow(serviceByNameSelect+` WHERE project_id = $1 AND name = $2`, projectID, name)
	return scanServiceByName(row)
}

func (r *ServiceRepository) listByName(name string, limit int) ([]*types.Service, error) {
	query := serviceByNameSelect + ` WHERE name = $1 ORDER BY created_at, id`
	args := []interface{}{name}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var services []*types.Service
	for rows.Next() {
		service, err := scanServiceByName(rows)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return services, rows.Err()
}

func scanServiceByName(row interface{ Scan(dest ...any) error }) (*types.Service, error) {
	service := &types.Service{}
	var buildConfigJSON []byte
	var jobsJSON []byte
	var appPath sql.NullString
	var k8sNamespace sql.NullString

	if err := row.Scan(
		&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
		&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
		&service.AutoDeployEnv, &k8sNamespace, &service.CreatedAt, &service.UpdatedAt, &jobsJSON,
		&service.Type, &service.Region,
	); err != nil {
		return nil, err
	}

	if appPath.Valid {
		service.AppPath = appPath.String
	}
	if k8sNamespace.Valid {
		ns := k8sNamespace.String
		service.K8sNamespace = &ns
	}
	if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
	}
	if len(jobsJSON) > 0 {
		if err := json.Unmarshal(jobsJSON, &service.Jobs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal jobs: %w", err)
		}
	}
	return service, nil
}
