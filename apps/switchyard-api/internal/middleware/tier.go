package middleware

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// tierLimit defines resource limits for a foundry tier.
type tierLimit struct {
	ProjectLimit int // -1 = unlimited
	ServiceLimit int // -1 = unlimited
}

// tierLimits mirrors the UI configuration in apps/switchyard-ui/lib/tiers.ts.
// community/essentials have identical feature limits — essentials value = managed hosting.
// Only pro+ features are gated. Legacy tier names kept for old JWTs during transition.
var tierLimits = map[string]tierLimit{
	// No claim = community self-hosted (same limits as essentials)
	"":           {ProjectLimit: 1, ServiceLimit: 3},
	"community":  {ProjectLimit: 1, ServiceLimit: 3},
	"essentials": {ProjectLimit: 1, ServiceLimit: 3},
	"pro":        {ProjectLimit: 10, ServiceLimit: -1},
	"madfam":     {ProjectLimit: -1, ServiceLimit: -1},
	// Legacy compat (old JWTs during transition)
	"sovereign": {ProjectLimit: 10, ServiceLimit: -1},
	"ecosystem": {ProjectLimit: -1, ServiceLimit: -1},
}

// getUpgradeURL returns the tier upgrade URL, preferring the ENCLII_TIER_UPGRADE_URL
// environment variable over the hardcoded default.
func getUpgradeURL() string {
	if url := os.Getenv("ENCLII_TIER_UPGRADE_URL"); url != "" {
		return url
	}
	return "https://dhanam.madfam.io/checkout?plan=enclii_pro&product=enclii"
}

var tierPaymentRequired = gin.H{
	"error":       "tier_limit_exceeded",
	"upgrade_url": getUpgradeURL(),
}

// limitsForTier returns the resource limits for the given tier string.
func limitsForTier(tier string) tierLimit {
	if l, ok := tierLimits[tier]; ok {
		return l
	}
	return tierLimits[""]
}

// resolveUserUUID attempts to parse user_id as a UUID.
// If it fails (external OIDC subject), it falls back to looking up the user
// by email so the tier check still applies to external identity providers.
func resolveUserUUID(c *gin.Context, repos *db.Repositories) (uuid.UUID, bool) {
	userID := c.GetString("user_id")
	if userID == "" {
		return uuid.UUID{}, false
	}

	uid, err := uuid.Parse(userID)
	if err == nil {
		return uid, true
	}

	// External OIDC users have a non-UUID subject. Look up via email.
	email := c.GetString("user_email")
	if email == "" {
		logrus.WithField("user_id", userID).Warn("Non-UUID user_id and no email in context, cannot resolve for tier check")
		return uuid.UUID{}, false
	}

	user, err := repos.Users.GetByEmail(c.Request.Context(), email)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"user_id": userID,
			"email":   email,
			"error":   err.Error(),
		}).Debug("Could not resolve external user by email for tier check")
		return uuid.UUID{}, false
	}

	return user.ID, true
}

// RequireTierForProject returns middleware that checks the user's foundry_tier
// before allowing project creation. Returns 402 if the tier limit is exceeded.
func RequireTierForProject(repos *db.Repositories) gin.HandlerFunc {
	return func(c *gin.Context) {
		limits := limitsForTier(c.GetString("foundry_tier"))

		if limits.ProjectLimit == -1 {
			c.Next()
			return
		}

		if limits.ProjectLimit == 0 {
			resp := gin.H{
				"error":       "tier_limit_exceeded",
				"message":     "Upgrade your plan to create projects.",
				"upgrade_url": tierPaymentRequired["upgrade_url"],
			}
			c.JSON(http.StatusPaymentRequired, resp)
			c.Abort()
			return
		}

		uid, ok := resolveUserUUID(c, repos)
		if !ok {
			// Cannot determine identity — fail open to avoid blocking legitimate requests
			c.Next()
			return
		}

		accessList, err := repos.ProjectAccess.ListByUser(c.Request.Context(), uid)
		if err != nil {
			logrus.WithError(err).Error("Failed to count user projects for tier check")
			c.Next()
			return
		}

		if len(accessList) >= limits.ProjectLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":       "tier_limit_exceeded",
				"message":     "You have reached your project limit. Upgrade your plan to create more projects.",
				"upgrade_url": tierPaymentRequired["upgrade_url"],
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireTierForService returns middleware that checks the user's foundry_tier
// before allowing service creation. Returns 402 if the tier limit is exceeded.
func RequireTierForService(repos *db.Repositories) gin.HandlerFunc {
	return func(c *gin.Context) {
		limits := limitsForTier(c.GetString("foundry_tier"))

		if limits.ServiceLimit == -1 {
			c.Next()
			return
		}

		if limits.ServiceLimit == 0 {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":       "tier_limit_exceeded",
				"message":     "Upgrade your plan to create services.",
				"upgrade_url": tierPaymentRequired["upgrade_url"],
			})
			c.Abort()
			return
		}

		projectSlug := c.Param("slug")
		if projectSlug == "" {
			c.Next()
			return
		}

		count, err := countServicesInProject(repos, projectSlug)
		if err != nil {
			// Let the handler deal with not-found / DB errors
			c.Next()
			return
		}

		if count >= limits.ServiceLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":       "tier_limit_exceeded",
				"message":     "You have reached your service limit. Upgrade your plan to deploy more services.",
				"upgrade_url": tierPaymentRequired["upgrade_url"],
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireTierForDeploy returns middleware that checks the user's foundry_tier
// before allowing deployments. Looks up the service's parent project and
// enforces the same service-count limit as service creation.
func RequireTierForDeploy(repos *db.Repositories) gin.HandlerFunc {
	return func(c *gin.Context) {
		limits := limitsForTier(c.GetString("foundry_tier"))

		if limits.ServiceLimit == -1 {
			c.Next()
			return
		}

		if limits.ServiceLimit == 0 {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":       "tier_limit_exceeded",
				"message":     "Upgrade your plan to deploy services.",
				"upgrade_url": tierPaymentRequired["upgrade_url"],
			})
			c.Abort()
			return
		}

		// Look up the service to find its parent project
		serviceIDStr := c.Param("id")
		if serviceIDStr == "" {
			c.Next()
			return
		}

		serviceID, err := uuid.Parse(serviceIDStr)
		if err != nil {
			c.Next()
			return
		}

		svc, err := repos.Services.GetByID(serviceID)
		if err != nil {
			c.Next()
			return
		}

		// Get the project to find its slug, then count services
		project, err := repos.Projects.GetByID(c.Request.Context(), svc.ProjectID)
		if err != nil {
			c.Next()
			return
		}

		services, err := repos.Services.ListByProject(project.ID)
		if err != nil {
			logrus.WithError(err).Error("Failed to count services for deploy tier check")
			c.Next()
			return
		}

		if len(services) > limits.ServiceLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":       "tier_limit_exceeded",
				"message":     "You have reached your service limit. Upgrade your plan to deploy more services.",
				"upgrade_url": tierPaymentRequired["upgrade_url"],
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// countServicesInProject returns the number of services in the project with the given slug.
func countServicesInProject(repos *db.Repositories, slug string) (int, error) {
	project, err := repos.Projects.GetBySlug(slug)
	if err != nil {
		return 0, err
	}

	services, err := repos.Services.ListByProject(project.ID)
	if err != nil {
		return 0, err
	}

	return len(services), nil
}
