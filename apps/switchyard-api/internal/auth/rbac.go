package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Role represents a user role in the system
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

// Permission represents a specific action that can be taken
type Permission string

const (
	// Project permissions
	PermissionProjectCreate Permission = "project:create"
	PermissionProjectRead   Permission = "project:read"
	PermissionProjectUpdate Permission = "project:update"
	PermissionProjectDelete Permission = "project:delete"

	// Service permissions
	PermissionServiceCreate Permission = "service:create"
	PermissionServiceRead   Permission = "service:read"
	PermissionServiceUpdate Permission = "service:update"
	PermissionServiceDelete Permission = "service:delete"

	// Deployment permissions
	PermissionDeploymentCreate   Permission = "deployment:create"
	PermissionDeploymentRead     Permission = "deployment:read"
	PermissionDeploymentRollback Permission = "deployment:rollback"

	// Build permissions
	PermissionBuildCreate Permission = "build:create"
	PermissionBuildRead   Permission = "build:read"

	// User management permissions
	PermissionUserList   Permission = "user:list"
	PermissionUserCreate Permission = "user:create"
	PermissionUserUpdate Permission = "user:update"
	PermissionUserDelete Permission = "user:delete"

	// Domain permissions
	PermissionDomainCreate Permission = "domain:create"
	PermissionDomainRead   Permission = "domain:read"
	PermissionDomainUpdate Permission = "domain:update"
	PermissionDomainDelete Permission = "domain:delete"
	PermissionDomainVerify Permission = "domain:verify"

	// Admin permissions
	PermissionAdminAccess Permission = "admin:access"
)

// rolePermissions defines the permissions for each role
var rolePermissions = map[Role][]Permission{
	RoleAdmin: {
		// Full access
		PermissionProjectCreate, PermissionProjectRead, PermissionProjectUpdate, PermissionProjectDelete,
		PermissionServiceCreate, PermissionServiceRead, PermissionServiceUpdate, PermissionServiceDelete,
		PermissionDeploymentCreate, PermissionDeploymentRead, PermissionDeploymentRollback,
		PermissionBuildCreate, PermissionBuildRead,
		PermissionUserList, PermissionUserCreate, PermissionUserUpdate, PermissionUserDelete,
		PermissionDomainCreate, PermissionDomainRead, PermissionDomainUpdate, PermissionDomainDelete, PermissionDomainVerify,
		PermissionAdminAccess,
	},
	RoleDeveloper: {
		// Read/write for projects, services, deployments
		PermissionProjectRead,
		PermissionServiceCreate, PermissionServiceRead, PermissionServiceUpdate,
		PermissionDeploymentCreate, PermissionDeploymentRead, PermissionDeploymentRollback,
		PermissionBuildCreate, PermissionBuildRead,
		PermissionDomainCreate, PermissionDomainRead, PermissionDomainUpdate, PermissionDomainVerify,
	},
	RoleViewer: {
		// Read-only access
		PermissionProjectRead,
		PermissionServiceRead,
		PermissionDeploymentRead,
		PermissionBuildRead,
		PermissionDomainRead,
	},
}

// HasPermission checks if a role has a specific permission
func HasPermission(role Role, permission Permission) bool {
	permissions, exists := rolePermissions[role]
	if !exists {
		return false
	}

	for _, p := range permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// HasAnyPermission checks if a role has any of the specified permissions
func HasAnyPermission(role Role, permissions ...Permission) bool {
	for _, p := range permissions {
		if HasPermission(role, p) {
			return true
		}
	}
	return false
}

// HasAllPermissions checks if a role has all of the specified permissions
func HasAllPermissions(role Role, permissions ...Permission) bool {
	for _, p := range permissions {
		if !HasPermission(role, p) {
			return false
		}
	}
	return true
}

// GetRolePermissions returns all permissions for a role
func GetRolePermissions(role Role) []Permission {
	return rolePermissions[role]
}

// =============================================================================
// Gin Middleware for RBAC
// =============================================================================

// RequirePermission returns a middleware that requires a specific permission
func RequirePermission(permission Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("user_role")
		if !exists {
			logrus.WithField("path", c.Request.URL.Path).Warn("RBAC: user_role not found in context")
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthorized",
			})
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			logrus.WithField("path", c.Request.URL.Path).Error("RBAC: invalid role format in context")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Internal server error",
			})
			c.Abort()
			return
		}

		if !HasPermission(Role(roleStr), permission) {
			logrus.WithFields(logrus.Fields{
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
				"role":       roleStr,
				"permission": permission,
				"user_id":    c.GetString("user_id"),
			}).Warn("RBAC: permission denied")

			c.JSON(http.StatusForbidden, gin.H{
				"error":      "Forbidden",
				"message":    "You don't have permission to perform this action",
				"permission": permission,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAnyPermission returns a middleware that requires any of the specified permissions
func RequireAnyPermission(permissions ...Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("user_role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			c.Abort()
			return
		}

		if !HasAnyPermission(Role(roleStr), permissions...) {
			logrus.WithFields(logrus.Fields{
				"path":        c.Request.URL.Path,
				"role":        roleStr,
				"permissions": permissions,
			}).Warn("RBAC: permission denied")

			c.JSON(http.StatusForbidden, gin.H{
				"error":   "Forbidden",
				"message": "You don't have permission to perform this action",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAllPermissions returns a middleware that requires all of the specified permissions
func RequireAllPermissions(permissions ...Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("user_role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			c.Abort()
			return
		}

		if !HasAllPermissions(Role(roleStr), permissions...) {
			logrus.WithFields(logrus.Fields{
				"path":        c.Request.URL.Path,
				"role":        roleStr,
				"permissions": permissions,
			}).Warn("RBAC: permission denied")

			c.JSON(http.StatusForbidden, gin.H{
				"error":   "Forbidden",
				"message": "You don't have all required permissions for this action",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAdmin is a convenience middleware that requires admin role
func RequireAdmin() gin.HandlerFunc {
	return RequirePermission(PermissionAdminAccess)
}

// RequireDeveloper is a convenience middleware that requires developer or admin role
func RequireDeveloper() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("user_role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			c.Abort()
			return
		}

		r := Role(roleStr)
		if r != RoleAdmin && r != RoleDeveloper {
			logrus.WithFields(logrus.Fields{
				"path": c.Request.URL.Path,
				"role": roleStr,
			}).Warn("RBAC: developer access denied")

			c.JSON(http.StatusForbidden, gin.H{
				"error":   "Forbidden",
				"message": "Developer or admin role required",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// =============================================================================
// Endpoint to Permission Mapping
// =============================================================================

// EndpointPermissions maps HTTP method + path pattern to required permission
var EndpointPermissions = map[string]map[string]Permission{
	"GET": {
		"/v1/projects":                     PermissionProjectRead,
		"/v1/projects/cards":               PermissionProjectRead,
		"/v1/projects/:slug":               PermissionProjectRead,
		"/v1/projects/:slug/services":      PermissionServiceRead,
		"/v1/services/:id":                 PermissionServiceRead,
		"/v1/services/:id/releases":        PermissionBuildRead,
		"/v1/services/:id/status":          PermissionServiceRead,
		"/v1/services/:id/deployments":     PermissionDeploymentRead,
		"/v1/deployments/:id":              PermissionDeploymentRead,
		"/v1/deployments/:id/logs":         PermissionDeploymentRead,
		"/v1/services/:id/domains":         PermissionDomainRead,
		"/v1/services/:id/domains/:domain": PermissionDomainRead,
		"/v1/users":                        PermissionUserList,
	},
	"POST": {
		"/v1/projects":                            PermissionProjectCreate,
		"/v1/projects/:slug/services":             PermissionServiceCreate,
		"/v1/services/:id/build":                  PermissionBuildCreate,
		"/v1/services/:id/deploy":                 PermissionDeploymentCreate,
		"/v1/deployments/:id/rollback":            PermissionDeploymentRollback,
		"/v1/services/:id/domains":                PermissionDomainCreate,
		"/v1/services/:id/domains/:domain/verify": PermissionDomainVerify,
	},
	"PATCH": {
		"/v1/services/:id/domains/:domain": PermissionDomainUpdate,
	},
	"DELETE": {
		"/v1/projects/:slug":               PermissionProjectDelete,
		"/v1/services/:id":                 PermissionServiceDelete,
		"/v1/services/:id/domains/:domain": PermissionDomainDelete,
	},
}

// GetRequiredPermission returns the required permission for a given method and path
func GetRequiredPermission(method, path string) (Permission, bool) {
	methodPerms, exists := EndpointPermissions[method]
	if !exists {
		return "", false
	}
	perm, exists := methodPerms[path]
	return perm, exists
}
