package api

import (
	"fmt"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// validateGeneratedCredentialRequest rejects a malformed generation request
// before any provisioning runs, so a bad flag never leaves half a project.
func validateGeneratedCredentialRequest(req *types.OnboardingRequest) error {
	owned := map[string]string{} // Secret key -> what writes it
	claim := func(key, by string) error {
		if err := provisioning.ValidateSecretKeyName(key); err != nil {
			return err
		}
		if prev, ok := owned[key]; ok {
			return fmt.Errorf("secret key %s is written by both %s and %s", key, prev, by)
		}
		owned[key] = by
		return nil
	}
	for _, k := range []string{provisioning.SecretKeyR2Bucket, provisioning.SecretKeyR2Endpoint,
		provisioning.SecretKeyR2AccessKeyID, provisioning.SecretKeyR2SecretAccessKey, provisioning.SecretKeyStorageBackend} {
		owned[k] = "R2 provisioning"
	}

	if pg := req.ProvisionPostgres; pg != nil {
		switch {
		case pg.GeneratePassword && pg.RolePassword != "":
			return fmt.Errorf("provision_postgres: role_password and generate_password are mutually exclusive")
		case !pg.GeneratePassword && pg.RolePassword == "":
			return fmt.Errorf("provision_postgres: role_password or generate_password is required")
		case !pg.GeneratePassword && (pg.RotatePassword || pg.URLSecretKey != ""):
			return fmt.Errorf("provision_postgres: rotate_password and url_secret_key need generate_password")
		}
		if pg.ConnectionLimit != 0 {
			if err := provisioning.ValidateConnectionLimit(pg.ConnectionLimit, "provision_postgres.connection_limit"); err != nil {
				return err
			}
		}
		if pg.GeneratePassword {
			key := pg.URLSecretKey
			if key == "" {
				key = defaultOwnerURLSecretKey
			}
			if err := claim(key, "provision_postgres"); err != nil {
				return err
			}
		}
	}

	if req.ProvisionAppRole != nil {
		spec := resolvedAppRole(req)
		if spec.DatabaseName == "" {
			return fmt.Errorf("provision_app_role: database_name is required when provision_postgres is absent")
		}
		owner := spec.DatabaseName
		if req.ProvisionPostgres != nil {
			owner = ownerRoleName(req.ProvisionPostgres)
		}
		if err := provisioning.ValidateAppRoleName(spec.RoleName, spec.DatabaseName, owner); err != nil {
			return err
		}
		if err := provisioning.ValidateConnectionLimit(spec.ConnectionLimit, "provision_app_role.connection_limit"); err != nil {
			return err
		}
		if err := claim(spec.URLSecretKey, "provision_app_role"); err != nil {
			return err
		}
		if spec.PasswordSecretKey != "" {
			if err := claim(spec.PasswordSecretKey, "provision_app_role"); err != nil {
				return err
			}
		}
	}

	for _, g := range req.GenerateSecrets {
		if g.Bytes != 0 && (g.Bytes < provisioning.MinGeneratedSecretBytes || g.Bytes > provisioning.MaxGeneratedSecretBytes) {
			return fmt.Errorf("generate_secrets: %s bytes %d out of range %d-%d", g.Key, g.Bytes,
				provisioning.MinGeneratedSecretBytes, provisioning.MaxGeneratedSecretBytes)
		}
		if err := claim(g.Key, "generate_secrets"); err != nil {
			return err
		}
	}

	// A secrets-file key must not shadow a generated value.
	for _, e := range req.ProvisionSecrets {
		if by, ok := owned[e.Key]; ok && by != "R2 provisioning" {
			return fmt.Errorf("provision_secrets key %s collides with the value %s generates", e.Key, by)
		}
	}
	return nil
}
