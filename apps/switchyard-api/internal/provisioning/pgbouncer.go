package provisioning

import (
	"context"
	"fmt"
	"strings"

	k8scorev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

const (
	pgbouncerNamespace    = "data"
	pgbouncerConfigMap    = "pgbouncer-config"
	pgbouncerUserlistName = "pgbouncer-userlist"
	pgbouncerDeployment   = "pgbouncer"
	pgbouncerConfigKey    = "pgbouncer.ini"
	pgbouncerUserKey      = "userlist.txt"
	pgbouncerHost         = "postgres.data.svc.cluster.local"
)

// PgBouncerUpdater manages PgBouncer configuration for new databases.
type PgBouncerUpdater struct {
	clientset kubernetes.Interface
	logger    logging.Logger
}

// NewPgBouncerUpdater creates a new PgBouncer updater using an existing K8s clientset.
func NewPgBouncerUpdater(clientset kubernetes.Interface, logger logging.Logger) *PgBouncerUpdater {
	return &PgBouncerUpdater{
		clientset: clientset,
		logger:    logger,
	}
}

// AddDatabase adds a database entry to PgBouncer config and the role to the userlist.
func (u *PgBouncerUpdater) AddDatabase(ctx context.Context, dbName, roleName, rolePassword string) error {
	if err := ValidateSQLIdentifier(dbName, "database_name"); err != nil {
		return err
	}
	if err := ValidateSQLIdentifier(roleName, "role_name"); err != nil {
		return err
	}

	// Update PgBouncer ConfigMap — add database entry
	if err := u.updateConfigMap(ctx, dbName); err != nil {
		return fmt.Errorf("update pgbouncer configmap: %w", err)
	}

	// Update PgBouncer userlist Secret — add role credentials
	if err := u.updateUserlist(ctx, roleName, rolePassword); err != nil {
		return fmt.Errorf("update pgbouncer userlist: %w", err)
	}

	// Trigger rollout restart
	if err := u.restartPgBouncer(ctx); err != nil {
		return fmt.Errorf("restart pgbouncer: %w", err)
	}

	u.logger.Info(ctx, "PgBouncer updated with new database",
		logging.String("database", dbName),
		logging.String("role", roleName))

	return nil
}

// updateConfigMap adds a database entry to the [databases] section of pgbouncer.ini.
func (u *PgBouncerUpdater) updateConfigMap(ctx context.Context, dbName string) error {
	cmClient := u.clientset.CoreV1().ConfigMaps(pgbouncerNamespace)
	cm, err := cmClient.Get(ctx, pgbouncerConfigMap, k8smetav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get configmap %s/%s: %w", pgbouncerNamespace, pgbouncerConfigMap, err)
	}

	ini, ok := cm.Data[pgbouncerConfigKey]
	if !ok {
		return fmt.Errorf("configmap %s missing key %s", pgbouncerConfigMap, pgbouncerConfigKey)
	}

	// Check if database already configured
	dbEntry := fmt.Sprintf("%s = host=%s dbname=%s", dbName, pgbouncerHost, dbName)
	if strings.Contains(ini, dbName+" =") || strings.Contains(ini, dbName+"=") {
		u.logger.Info(ctx, "Database already in PgBouncer config", logging.String("database", dbName))
		return nil
	}

	// Insert after [databases] header
	marker := "[databases]"
	idx := strings.Index(ini, marker)
	if idx < 0 {
		return fmt.Errorf("pgbouncer.ini missing [databases] section")
	}
	insertPos := idx + len(marker)
	// Find end of line after [databases]
	nlIdx := strings.Index(ini[insertPos:], "\n")
	if nlIdx >= 0 {
		insertPos += nlIdx + 1
	} else {
		ini += "\n"
		insertPos = len(ini)
	}

	cm.Data[pgbouncerConfigKey] = ini[:insertPos] + dbEntry + "\n" + ini[insertPos:]

	_, err = cmClient.Update(ctx, cm, k8smetav1.UpdateOptions{})
	return err
}

// updateUserlist adds a role entry to the PgBouncer userlist secret.
// If the secret doesn't exist, it bootstraps it with the given role.
func (u *PgBouncerUpdater) updateUserlist(ctx context.Context, roleName, rolePassword string) error {
	secretClient := u.clientset.CoreV1().Secrets(pgbouncerNamespace)
	secret, err := secretClient.Get(ctx, pgbouncerUserlistName, k8smetav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		// Bootstrap: create the secret with just this role
		entry := fmt.Sprintf("\"%s\" \"%s\"\n", roleName, rolePassword)
		newSecret := &k8scorev1.Secret{
			ObjectMeta: k8smetav1.ObjectMeta{
				Name:      pgbouncerUserlistName,
				Namespace: pgbouncerNamespace,
				Labels: map[string]string{
					"enclii.dev/managed-by": "provisioning-api",
				},
			},
			Data: map[string][]byte{
				pgbouncerUserKey: []byte(entry),
			},
		}
		_, createErr := secretClient.Create(ctx, newSecret, k8smetav1.CreateOptions{})
		if createErr != nil {
			return fmt.Errorf("bootstrap secret %s/%s: %w", pgbouncerNamespace, pgbouncerUserlistName, createErr)
		}
		u.logger.Info(ctx, "Bootstrapped PgBouncer userlist secret",
			logging.String("role", roleName))
		return nil
	}
	if err != nil {
		return fmt.Errorf("get secret %s/%s: %w", pgbouncerNamespace, pgbouncerUserlistName, err)
	}

	userlist := string(secret.Data[pgbouncerUserKey])

	// Check if role already in userlist
	if strings.Contains(userlist, "\""+roleName+"\"") {
		u.logger.Info(ctx, "Role already in PgBouncer userlist", logging.String("role", roleName))
		return nil
	}

	// PgBouncer userlist format: "username" "password"
	entry := fmt.Sprintf("\"%s\" \"%s\"\n", roleName, rolePassword)
	userlist += entry
	secret.Data[pgbouncerUserKey] = []byte(userlist)

	_, err = secretClient.Update(ctx, secret, k8smetav1.UpdateOptions{})
	return err
}

// restartPgBouncer triggers a rollout restart by patching the deployment annotation.
func (u *PgBouncerUpdater) restartPgBouncer(ctx context.Context) error {
	patch := []byte(`{"spec":{"template":{"metadata":{"annotations":{"enclii.dev/restartedAt":"` +
		k8smetav1.Now().Format("2006-01-02T15:04:05Z") + `"}}}}}`)

	_, err := u.clientset.AppsV1().Deployments(pgbouncerNamespace).Patch(
		ctx,
		pgbouncerDeployment,
		k8stypes.StrategicMergePatchType,
		patch,
		k8smetav1.PatchOptions{},
	)
	return err
}
