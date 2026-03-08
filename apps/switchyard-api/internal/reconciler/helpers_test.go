package reconciler

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// getAddonPort
// ---------------------------------------------------------------------------

func TestGetAddonPort(t *testing.T) {
	tests := []struct {
		name      string
		addonType types.DatabaseAddonType
		wantPort  int32
	}{
		{
			name:      "postgres returns 5432",
			addonType: types.DatabaseAddonTypePostgres,
			wantPort:  5432,
		},
		{
			name:      "redis returns 6379",
			addonType: types.DatabaseAddonTypeRedis,
			wantPort:  6379,
		},
		{
			name:      "mysql returns 3306",
			addonType: types.DatabaseAddonTypeMySQL,
			wantPort:  3306,
		},
		{
			name:      "unknown type defaults to 5432 (postgres)",
			addonType: types.DatabaseAddonType("unknown"),
			wantPort:  5432,
		},
		{
			name:      "empty string defaults to 5432",
			addonType: types.DatabaseAddonType(""),
			wantPort:  5432,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getAddonPort(tt.addonType)
			assert.Equal(t, tt.wantPort, got)
		})
	}
}

// ---------------------------------------------------------------------------
// sanitizeDomainForSecret
// ---------------------------------------------------------------------------

func TestSanitizeDomainForSecret(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string
	}{
		{
			name:   "simple domain with dots replaced by dashes",
			domain: "api.enclii.dev",
			want:   "api-enclii-dev",
		},
		{
			name:   "subdomain with multiple dots",
			domain: "status.madfam.io",
			want:   "status-madfam-io",
		},
		{
			name:   "domain with no dots",
			domain: "localhost",
			want:   "localhost",
		},
		{
			name:   "empty string returns empty",
			domain: "",
			want:   "",
		},
		{
			name:   "deep subdomain",
			domain: "a.b.c.d.example.com",
			want:   "a-b-c-d-example-com",
		},
		{
			name:   "single character domain parts",
			domain: "a.b",
			want:   "a-b",
		},
		{
			name:   "trailing dot",
			domain: "example.com.",
			want:   "example-com-",
		},
		{
			name:   "leading dot",
			domain: ".example.com",
			want:   "-example-com",
		},
		{
			name:   "consecutive dots",
			domain: "a..b.com",
			want:   "a--b-com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeDomainForSecret(tt.domain)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// stringPtr / protocolPtr
// ---------------------------------------------------------------------------

func TestStringPtr(t *testing.T) {
	val := "hello"
	ptr := stringPtr(val)
	require.NotNil(t, ptr)
	assert.Equal(t, val, *ptr)

	// Verify it is a new pointer, not aliased to the original stack variable
	*ptr = "changed"
	assert.Equal(t, "hello", val, "modifying returned pointer must not affect original")
}

func TestStringPtr_Empty(t *testing.T) {
	ptr := stringPtr("")
	require.NotNil(t, ptr)
	assert.Equal(t, "", *ptr)
}

func TestProtocolPtr(t *testing.T) {
	p := corev1.ProtocolTCP
	ptr := protocolPtr(p)
	require.NotNil(t, ptr)
	assert.Equal(t, corev1.ProtocolTCP, *ptr)
}

func TestProtocolPtr_UDP(t *testing.T) {
	p := corev1.ProtocolUDP
	ptr := protocolPtr(p)
	require.NotNil(t, ptr)
	assert.Equal(t, corev1.ProtocolUDP, *ptr)
}

// ---------------------------------------------------------------------------
// buildAddonEnvVars
// ---------------------------------------------------------------------------

func TestBuildAddonEnvVars_PostgresWithCustomSecret(t *testing.T) {
	bindings := []AddonBinding{
		{
			EnvVarName:       "DATABASE_URL",
			AddonType:        types.DatabaseAddonTypePostgres,
			K8sNamespace:     "myproject",
			K8sResourceName:  "mydb-cluster",
			ConnectionSecret: "custom-pg-secret",
		},
	}

	envVars := buildAddonEnvVars(bindings)

	require.Len(t, envVars, 1)
	assert.Equal(t, "DATABASE_URL", envVars[0].Name)
	assert.Empty(t, envVars[0].Value, "postgres should use secretKeyRef, not inline value")
	require.NotNil(t, envVars[0].ValueFrom)
	require.NotNil(t, envVars[0].ValueFrom.SecretKeyRef)
	assert.Equal(t, "custom-pg-secret", envVars[0].ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "uri", envVars[0].ValueFrom.SecretKeyRef.Key)
}

func TestBuildAddonEnvVars_PostgresDefaultSecret(t *testing.T) {
	bindings := []AddonBinding{
		{
			EnvVarName:       "DATABASE_URL",
			AddonType:        types.DatabaseAddonTypePostgres,
			K8sNamespace:     "myproject",
			K8sResourceName:  "mydb-cluster",
			ConnectionSecret: "", // empty triggers default naming
		},
	}

	envVars := buildAddonEnvVars(bindings)

	require.Len(t, envVars, 1)
	require.NotNil(t, envVars[0].ValueFrom)
	require.NotNil(t, envVars[0].ValueFrom.SecretKeyRef)
	assert.Equal(t, "mydb-cluster-app", envVars[0].ValueFrom.SecretKeyRef.Name,
		"default CloudNativePG secret name should be <resource>-app")
	assert.Equal(t, "uri", envVars[0].ValueFrom.SecretKeyRef.Key)
}

func TestBuildAddonEnvVars_Redis(t *testing.T) {
	bindings := []AddonBinding{
		{
			EnvVarName:      "REDIS_URL",
			AddonType:       types.DatabaseAddonTypeRedis,
			K8sNamespace:    "myproject",
			K8sResourceName: "myredis",
		},
	}

	envVars := buildAddonEnvVars(bindings)

	require.Len(t, envVars, 1)
	assert.Equal(t, "REDIS_URL", envVars[0].Name)
	assert.Nil(t, envVars[0].ValueFrom, "redis should use inline value, not secretKeyRef")
	assert.Equal(t, "redis://myredis.myproject.svc.cluster.local:6379/0", envVars[0].Value)
}

func TestBuildAddonEnvVars_MySQLWithCustomSecret(t *testing.T) {
	bindings := []AddonBinding{
		{
			EnvVarName:       "MYSQL_URL",
			AddonType:        types.DatabaseAddonTypeMySQL,
			K8sNamespace:     "myproject",
			K8sResourceName:  "mymysql",
			ConnectionSecret: "mysql-creds",
		},
	}

	envVars := buildAddonEnvVars(bindings)

	require.Len(t, envVars, 1)
	assert.Equal(t, "MYSQL_URL", envVars[0].Name)
	require.NotNil(t, envVars[0].ValueFrom)
	require.NotNil(t, envVars[0].ValueFrom.SecretKeyRef)
	assert.Equal(t, "mysql-creds", envVars[0].ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "uri", envVars[0].ValueFrom.SecretKeyRef.Key)
}

func TestBuildAddonEnvVars_MySQLDefaultSecret(t *testing.T) {
	bindings := []AddonBinding{
		{
			EnvVarName:       "MYSQL_URL",
			AddonType:        types.DatabaseAddonTypeMySQL,
			K8sNamespace:     "myproject",
			K8sResourceName:  "mymysql",
			ConnectionSecret: "",
		},
	}

	envVars := buildAddonEnvVars(bindings)

	require.Len(t, envVars, 1)
	require.NotNil(t, envVars[0].ValueFrom)
	require.NotNil(t, envVars[0].ValueFrom.SecretKeyRef)
	assert.Equal(t, "mymysql-credentials", envVars[0].ValueFrom.SecretKeyRef.Name,
		"default MySQL secret name should be <resource>-credentials")
}

func TestBuildAddonEnvVars_MultipleBindings(t *testing.T) {
	bindings := []AddonBinding{
		{
			EnvVarName:       "DATABASE_URL",
			AddonType:        types.DatabaseAddonTypePostgres,
			K8sNamespace:     "prod",
			K8sResourceName:  "pg-main",
			ConnectionSecret: "pg-main-app",
		},
		{
			EnvVarName:      "REDIS_URL",
			AddonType:       types.DatabaseAddonTypeRedis,
			K8sNamespace:    "prod",
			K8sResourceName: "redis-cache",
		},
		{
			EnvVarName:       "MYSQL_URL",
			AddonType:        types.DatabaseAddonTypeMySQL,
			K8sNamespace:     "prod",
			K8sResourceName:  "mysql-legacy",
			ConnectionSecret: "",
		},
	}

	envVars := buildAddonEnvVars(bindings)

	require.Len(t, envVars, 3)

	// Verify each binding produced the correct env var name in order
	assert.Equal(t, "DATABASE_URL", envVars[0].Name)
	assert.Equal(t, "REDIS_URL", envVars[1].Name)
	assert.Equal(t, "MYSQL_URL", envVars[2].Name)

	// Postgres uses secret ref
	require.NotNil(t, envVars[0].ValueFrom)

	// Redis uses inline value
	assert.Contains(t, envVars[1].Value, "redis://redis-cache.prod.svc.cluster.local:6379/0")

	// MySQL uses secret ref with default name
	require.NotNil(t, envVars[2].ValueFrom)
	assert.Equal(t, "mysql-legacy-credentials", envVars[2].ValueFrom.SecretKeyRef.Name)
}

func TestBuildAddonEnvVars_EmptyBindings(t *testing.T) {
	envVars := buildAddonEnvVars(nil)
	assert.Nil(t, envVars)

	envVars = buildAddonEnvVars([]AddonBinding{})
	assert.Nil(t, envVars)
}
