package workloadtargets

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// InsertKarafielStagingService runs under the existing project lifecycle lock.
// It is not an upsert: any incumbent ID/name/namespace claim refuses adoption.
// The caller must Persist the native043 binding/event before committing.
func (s *Session) InsertKarafielStagingService(ctx context.Context, operation uuid.UUID, service *types.Service) error {
	if s == nil || s.tx == nil || operation == uuid.Nil || s.serviceID != uuid.Nil ||
		ValidateKarafielStagingService(service, s.projectID) != nil || service.ID != KarafielStagingServiceID(s.projectID, operation) ||
		service.BuildConfig.Type != types.BuildTypeDockerfile || service.BuildConfig.Dockerfile != "apps/api/Dockerfile" || service.BuildConfig.Context != "." ||
		service.BuildConfig.BuildOnly || len(service.BuildConfig.BuildArgs) != 0 || service.BuildConfig.Target != "" || service.BuildConfig.Buildpack != "" {
		return ErrIntent
	}
	var conflict bool
	if s.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE id=$1 OR (project_id=$2 AND name=$3) OR k8s_namespace=$4)`,
		service.ID, s.projectID, KarafielStagingServiceName, KarafielStagingNamespace).Scan(&conflict) != nil {
		return ErrUnavailable
	}
	if conflict {
		return ErrConflict
	}
	config, err := json.Marshal(service.BuildConfig)
	if err != nil {
		return ErrIntent
	}
	_, err = s.tx.ExecContext(ctx, `INSERT INTO services (id,project_id,name,git_repo,app_path,build_config,volumes,auto_deploy,auto_deploy_branch,auto_deploy_env,k8s_namespace,jobs,type,region) VALUES($1,$2,$3,$4,$5,$6,'[]'::jsonb,false,$7,'staging',$8,'[]'::jsonb,'web','default')`,
		service.ID, s.projectID, KarafielStagingServiceName, service.GitRepo, service.AppPath, config, "release/karafiel-staging", KarafielStagingNamespace)
	if err != nil {
		return ErrConflict
	}
	return s.SelectServiceForBinding(ctx, service.ID)
}
