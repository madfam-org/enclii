package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TestLoadWorkloadInputsStrictness: the deploy path (lenient) keeps going on
// a failed read; the domain path (strict) must stop instead of re-applying a
// running Deployment without its env vars or addon DSNs.
func TestLoadWorkloadInputsStrictness(t *testing.T) {
	readErr := errors.New("connection reset")
	bindingCols := []string{"id", "addon_id", "service_id", "env_var_name", "status", "created_at", "updated_at"}
	envMetaCols := []string{"key", "value_encrypted", "is_secret", "environment_id"}

	tests := []struct {
		name   string
		mocks  func(m sqlmock.Sqlmock, svc uuid.UUID)
		strict bool
		// lenient expectations
		wantErr      bool
		wantBindings int
	}{
		{
			name: "env var reads both fail: strict stops",
			mocks: func(m sqlmock.Sqlmock, _ uuid.UUID) {
				m.ExpectQuery(`FROM environment_variables`).WillReturnError(readErr)
				m.ExpectQuery(`FROM environment_variables`).WillReturnError(readErr)
			},
			strict:  true,
			wantErr: true,
		},
		{
			name: "env var reads both fail: lenient continues",
			mocks: func(m sqlmock.Sqlmock, _ uuid.UUID) {
				m.ExpectQuery(`FROM environment_variables`).WillReturnError(readErr)
				m.ExpectQuery(`FROM environment_variables`).WillReturnError(readErr)
				m.ExpectQuery(`FROM database_addon_bindings`).WillReturnRows(sqlmock.NewRows(bindingCols))
			},
		},
		{
			name: "addon binding read fails: strict stops",
			mocks: func(m sqlmock.Sqlmock, _ uuid.UUID) {
				m.ExpectQuery(`FROM environment_variables`).WillReturnRows(sqlmock.NewRows(envMetaCols))
				m.ExpectQuery(`FROM database_addon_bindings`).WillReturnError(readErr)
			},
			strict:  true,
			wantErr: true,
		},
		{
			name: "addon binding read fails: lenient continues without bindings",
			mocks: func(m sqlmock.Sqlmock, _ uuid.UUID) {
				m.ExpectQuery(`FROM environment_variables`).WillReturnRows(sqlmock.NewRows(envMetaCols))
				m.ExpectQuery(`FROM database_addon_bindings`).WillReturnError(readErr)
			},
		},
		{
			name: "addon behind a binding cannot be read: strict stops",
			mocks: func(m sqlmock.Sqlmock, svc uuid.UUID) {
				m.ExpectQuery(`FROM environment_variables`).WillReturnRows(sqlmock.NewRows(envMetaCols))
				m.ExpectQuery(`FROM database_addon_bindings`).WillReturnRows(sqlmock.NewRows(bindingCols).
					AddRow(uuid.New(), uuid.New(), svc, "DATABASE_URL", "active", time.Now(), time.Now()))
				m.ExpectQuery(`FROM database_addons`).WillReturnError(readErr)
			},
			strict:  true,
			wantErr: true,
		},
		{
			name: "addon behind a binding cannot be read: lenient skips it",
			mocks: func(m sqlmock.Sqlmock, svc uuid.UUID) {
				m.ExpectQuery(`FROM environment_variables`).WillReturnRows(sqlmock.NewRows(envMetaCols))
				m.ExpectQuery(`FROM database_addon_bindings`).WillReturnRows(sqlmock.NewRows(bindingCols).
					AddRow(uuid.New(), uuid.New(), svc, "DATABASE_URL", "active", time.Now(), time.Now()))
				m.ExpectQuery(`FROM database_addons`).WillReturnError(readErr)
			},
		},
		{
			name: "all reads succeed: strict returns the inputs",
			mocks: func(m sqlmock.Sqlmock, _ uuid.UUID) {
				m.ExpectQuery(`FROM environment_variables`).WillReturnRows(sqlmock.NewRows(envMetaCols))
				m.ExpectQuery(`FROM database_addon_bindings`).WillReturnRows(sqlmock.NewRows(bindingCols))
			},
			strict: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close() }()
			svc := &types.Service{ID: uuid.New(), ProjectID: uuid.New(), Name: "web"}
			env := &types.Environment{ID: uuid.New(), KubeNamespace: "ns"}
			dep := &types.Deployment{ID: uuid.New(), EnvironmentID: env.ID}
			tt.mocks(mock, svc.ID)

			repos := &db.Repositories{
				EnvVars:        db.NewEnvVarRepository(database),
				DatabaseAddons: db.NewDatabaseAddonRepository(database),
			}
			c := &Controller{}
			logger := logrus.New()
			logger.SetLevel(logrus.PanicLevel)

			in := &ReconcileRequest{}
			err = c.loadWorkloadInputs(context.Background(), logrus.NewEntry(logger), repos, svc, env, dep, tt.strict, in)
			if tt.wantErr {
				if err == nil || !errors.Is(err, readErr) {
					t.Fatalf("err = %v, want the read error", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if in.EnvVars == nil {
					t.Fatalf("inputs = %+v, want a non-nil env map", in)
				}
				if len(in.AddonBindings) != tt.wantBindings {
					t.Fatalf("bindings = %d, want %d", len(in.AddonBindings), tt.wantBindings)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReconcileDomainsWithoutReconcilerFailsVisibly(t *testing.T) {
	var c *Controller
	res := c.ReconcileDomains(context.Background(), uuid.New(), uuid.New())
	if res == nil || res.Success || res.Error == nil {
		t.Fatalf("result = %+v, want a failure", res)
	}
}
