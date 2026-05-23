package config

import "os"

// IsProduction reports whether the process is running in a production deployment.
func (c *Config) IsProduction() bool {
	if c == nil {
		return false
	}
	if c.Environment == "production" {
		return true
	}
	return os.Getenv("ENCLII_ENV") == "production"
}

// AllowsUnauthenticatedInternalCallbacks is true only in local/bootstrap environments
// where Roundhouse may run without a shared API key configured.
func (c *Config) AllowsUnauthenticatedInternalCallbacks() bool {
	return !c.IsProduction() && (c.AuthMode == "local" || c.AuthMode == "" || c.Environment == "development" || c.Environment == "local" || c.Environment == "")
}
