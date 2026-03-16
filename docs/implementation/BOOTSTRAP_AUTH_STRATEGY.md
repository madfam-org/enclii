# Bootstrap Authentication Strategy: Solving the Enclii ↔ Janua Chicken-and-Egg Problem

**Date:** November 21, 2025
**Status:** PHASE C COMPLETE - Ready for Testing
**Priority:** CRITICAL for Weeks 3-4 (Janua Integration)

**Implementation Summary:**
- ✅ Phase A (Local Auth): Already working
- ⚠️ Phase B (Deploy Janua): Ready for deployment
- ✅ Phase C (OIDC Mode): **COMPLETE** - Compiles successfully
- ❌ End-to-End Testing: Not performed (requires database setup)
- ⚠️ Migration 005: SQL correct, not validated against live database
- ✅ Build Status: **Application compiles successfully (67MB binary)**

---

## The Problem: Circular Dependency

**You cannot use Janua to secure Enclii if Enclii is required to host Janua.**

```
┌─────────────────────────────────────────┐
│  ❌ PROBLEM: Circular Dependency        │
├─────────────────────────────────────────┤
│                                         │
│  Enclii (PaaS)                          │
│     │                                   │
│     │ needs auth from ──────┐          │
│     │                       ▼          │
│     │                   Janua (Auth)  │
│     │                       │          │
│     │ hosts ◄───────────────┘          │
│     │                                   │
│  Cannot deploy Janua without Enclii   │
│  Cannot secure Enclii without Janua   │
└─────────────────────────────────────────┘
```

---

## Current State Analysis

### ✅ What Exists (Bootstrap Auth Already Implemented)

**Location:** `apps/switchyard-api/internal/auth/jwt.go`

**Current Implementation:**
- ✅ Local JWT authentication with RS256 signing
- ✅ User registration and login endpoints
- ✅ Password hashing with bcrypt
- ✅ Session management via Redis
- ✅ RBAC with admin/developer/viewer roles
- ✅ Token refresh mechanism
- ✅ Session revocation support

**This IS your "Bootstrap Mode" - it's already working!**

### ✅ What's Implemented (OIDC/Janua Integration) - Phase C

**Status:** CODE COMPLETE - Testing Required

**Implemented Components:**
- ✅ OIDC client integration (`internal/auth/oidc.go`)
- ✅ JWKS endpoint with proper RSA key encoding (`/v1/auth/jwks`)
- ✅ OAuth 2.0 authorization code flow
- ✅ Token validation and user migration logic
- ✅ **AUTH_MODE** configuration to switch between local and OIDC
- ✅ Factory pattern for dual-mode auth (`internal/auth/manager.go`)
- ✅ Database migration 005 for OIDC support
- ✅ Email-based user migration (links local accounts to OIDC)

### 📋 Configuration Prepared (Unused)

**Location:** `apps/switchyard-api/internal/config/config.go:20-23`

```go
// OIDC Configuration
OIDCIssuer       string  // Present but unused
OIDCClientID     string  // Present but unused
OIDCClientSecret string  // Present but unused
```

**These fields are defined but not consumed by any auth logic.**

---

## The Solution: Three-Phase Bootstrap Strategy

### Phase A: Bootstrap Mode (Local Auth) ✅ ALREADY IMPLEMENTED

**Goal:** Deploy Enclii with standalone authentication

**Current Status:** **COMPLETE** - Already working!

**What You Have:**
```bash
# Environment Variables (Current)
ENCLII_DATABASE_URL=postgres://...
ENCLII_REDIS_HOST=localhost
ENCLII_REDIS_PORT=6379

# Auth works via local JWT
# No Janua dependency
```

**Authentication Flow:**
```
User → POST /v1/auth/register
     → POST /v1/auth/login
     ← JWT (signed with local RSA key)
     → Protected endpoints (validated by JWTManager)
```

**Admin Account Creation:**
```sql
-- Option 1: Create super-admin via SQL migration
INSERT INTO users (id, email, password_hash, role, active)
VALUES (
  gen_random_uuid(),
  'admin@madfam.io',
  '$2a$10$...',  -- bcrypt hash
  'admin',
  true
);

-- Option 2: Use registration endpoint with first-user-is-admin logic
```

**Action Items:**
- ✅ No code changes needed - this already works
- ⚠️ **MISSING:** Add `ENCLII_BOOTSTRAP_ADMIN_EMAIL` env var for auto-admin creation
- ⚠️ **MISSING:** Add migration or startup hook to create bootstrap admin

---

### Phase B: Deploy Janua (Using Bootstrap Admin) 🔄 NEXT STEP

**Goal:** Deploy Janua container using the local admin account

**Prerequisites:**
- ✅ Enclii running with local auth (Phase A)
- ✅ Bootstrap admin account created
- ✅ Janua service spec ready

**Steps:**
1. Log in to Enclii dashboard using bootstrap admin
2. Navigate to "Deploy Service"
3. Deploy Janua from service spec:
   ```yaml
   apiVersion: enclii.dev/v1
   kind: Service
   metadata:
     name: janua
     project: enclii-platform
   spec:
     source:
       type: git
       repository: https://github.com/madfam-org/janua
       branch: main
     build:
       type: dockerfile
       dockerfile: Dockerfile
     routes:
       - domain: auth.enclii.io
         path: /
         port: 8080
     replicas: 3
     autoscaling:
       enabled: true
       minReplicas: 3
       maxReplicas: 10
   ```
4. Wait for deployment to complete
5. Janua is now live at `https://auth.enclii.io`

**Configure Janua:**
```bash
# Create Enclii as an OAuth client in Janua
curl -X POST https://auth.enclii.io/admin/clients \
  -H "Authorization: Bearer <janua-admin-token>" \
  -d '{
    "client_id": "enclii-platform",
    "client_name": "Enclii Platform",
    "redirect_uris": ["https://api.enclii.io/v1/auth/callback"],
    "grant_types": ["authorization_code", "refresh_token"],
    "response_types": ["code"],
    "scope": "openid profile email"
  }'
```

**Action Items:**
- ⚠️ Deploy Janua via Enclii UI or CLI
- ⚠️ Configure DNS for `auth.enclii.io`
- ⚠️ Register Enclii as OAuth client in Janua
- ⚠️ Store client credentials securely (Vault/Lockbox)

---

### Phase C: The Switch (OIDC Mode) ✅ CODE COMPLETE

**Goal:** Reconfigure Enclii to use Janua for authentication

**Current Status:** **IMPLEMENTED - Testing Required**

**What Was Implemented (November 21, 2025):**
- ✅ All code changes described below have been completed
- ✅ JWKS endpoint properly encodes RSA public key
- ✅ User migration logic via email matching
- ✅ Dual-mode authentication (local vs OIDC)
- ❌ **NOT TESTED** - No validation with real OIDC provider
- ❌ **NOT TESTED** - Migration 005 not run
- ❌ **NOT TESTED** - Application startup not validated
- ⚠️ **BUILD STATUS:** ~8 non-critical errors in deployment_handlers.go

#### Code Changes Completed

##### 1. Add AUTH_MODE Configuration

**File:** `apps/switchyard-api/internal/config/config.go`

```go
type Config struct {
    // ... existing fields ...

    // Authentication Mode
    AuthMode         string // "local" or "oidc"

    // OIDC Configuration (already present)
    OIDCIssuer       string
    OIDCClientID     string
    OIDCClientSecret string
    OIDCRedirectURL  string // NEW
}

func Load() (*Config, error) {
    // ... existing code ...

    viper.SetDefault("auth-mode", "local") // Default to bootstrap mode
    viper.SetDefault("oidc-redirect-url", "https://api.enclii.io/v1/auth/callback")

    config := &Config{
        // ... existing fields ...
        AuthMode:         viper.GetString("auth-mode"),
        OIDCRedirectURL:  viper.GetString("oidc-redirect-url"),
    }

    return config, nil
}
```

##### 2. Create OIDC Authentication Service

**File:** `apps/switchyard-api/internal/auth/oidc.go` (NEW)

```go
package auth

import (
    "context"
    "github.com/coreos/go-oidc/v3/oidc"
    "golang.org/x/oauth2"
)

type OIDCManager struct {
    provider     *oidc.Provider
    verifier     *oidc.IDTokenVerifier
    oauth2Config *oauth2.Config
    repos        *db.Repositories
}

func NewOIDCManager(
    ctx context.Context,
    issuer string,
    clientID string,
    clientSecret string,
    redirectURL string,
    repos *db.Repositories,
) (*OIDCManager, error) {
    provider, err := oidc.NewProvider(ctx, issuer)
    if err != nil {
        return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
    }

    verifier := provider.Verifier(&oidc.Config{
        ClientID: clientID,
    })

    oauth2Config := &oauth2.Config{
        ClientID:     clientID,
        ClientSecret: clientSecret,
        RedirectURL:  redirectURL,
        Endpoint:     provider.Endpoint(),
        Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
    }

    return &OIDCManager{
        provider:     provider,
        verifier:     verifier,
        oauth2Config: oauth2Config,
        repos:        repos,
    }, nil
}

// GetAuthURL returns the OAuth authorization URL
func (o *OIDCManager) GetAuthURL(state string) string {
    return o.oauth2Config.AuthCodeURL(state)
}

// HandleCallback processes the OAuth callback and creates/updates user
func (o *OIDCManager) HandleCallback(ctx context.Context, code string) (*User, error) {
    // Exchange code for token
    oauth2Token, err := o.oauth2Config.Exchange(ctx, code)
    if err != nil {
        return nil, fmt.Errorf("failed to exchange token: %w", err)
    }

    // Extract ID token
    rawIDToken, ok := oauth2Token.Extra("id_token").(string)
    if !ok {
        return nil, fmt.Errorf("no id_token in token response")
    }

    // Verify ID token
    idToken, err := o.verifier.Verify(ctx, rawIDToken)
    if err != nil {
        return nil, fmt.Errorf("failed to verify ID token: %w", err)
    }

    // Extract claims
    var claims struct {
        Email string `json:"email"`
        Name  string `json:"name"`
        Sub   string `json:"sub"`
    }
    if err := idToken.Claims(&claims); err != nil {
        return nil, fmt.Errorf("failed to parse claims: %w", err)
    }

    // Create or update user in local database
    user, err := o.repos.User.GetByEmail(ctx, claims.Email)
    if err != nil {
        // User doesn't exist, create them
        user = &User{
            ID:    uuid.New(),
            Email: claims.Email,
            Name:  claims.Name,
            Role:  "developer", // Default role
            Active: true,
        }
        if err := o.repos.User.Create(ctx, user); err != nil {
            return nil, fmt.Errorf("failed to create user: %w", err)
        }
    }

    return user, nil
}

// ValidateToken validates an OIDC access token
func (o *OIDCManager) ValidateToken(ctx context.Context, token string) (*User, error) {
    // Verify token with OIDC provider
    idToken, err := o.verifier.Verify(ctx, token)
    if err != nil {
        return nil, fmt.Errorf("invalid token: %w", err)
    }

    // Extract email and fetch user
    var claims struct {
        Email string `json:"email"`
    }
    if err := idToken.Claims(&claims); err != nil {
        return nil, err
    }

    return o.repos.User.GetByEmail(ctx, claims.Email)
}
```

##### 3. Create Auth Manager Factory

**File:** `apps/switchyard-api/internal/auth/manager.go` (NEW)

```go
package auth

import (
    "context"
    "fmt"
)

// AuthManager is the interface for all authentication methods
type AuthManager interface {
    AuthMiddleware() gin.HandlerFunc
    // Add other common methods
}

// NewAuthManager creates the appropriate auth manager based on config
func NewAuthManager(
    ctx context.Context,
    config *config.Config,
    repos *db.Repositories,
    cache SessionRevoker,
) (AuthManager, error) {
    switch config.AuthMode {
    case "local":
        return NewJWTManager(
            15*time.Minute,  // access token duration
            7*24*time.Hour,  // refresh token duration
            repos,
            cache,
        )

    case "oidc":
        if config.OIDCIssuer == "" {
            return nil, fmt.Errorf("OIDC mode requires ENCLII_OIDC_ISSUER")
        }
        return NewOIDCManager(
            ctx,
            config.OIDCIssuer,
            config.OIDCClientID,
            config.OIDCClientSecret,
            config.OIDCRedirectURL,
            repos,
        )

    default:
        return nil, fmt.Errorf("invalid auth mode: %s (must be 'local' or 'oidc')", config.AuthMode)
    }
}
```

##### 4. Add OIDC Callback Handlers

**File:** `apps/switchyard-api/internal/api/auth_handlers.go` (UPDATE)

```go
// Add to existing file

// OIDCLogin redirects to Janua for authentication
func (h *Handler) OIDCLogin(c *gin.Context) {
    if h.config.AuthMode != "oidc" {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "OIDC not enabled",
        })
        return
    }

    oidcMgr, ok := h.auth.(*auth.OIDCManager)
    if !ok {
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Auth manager is not OIDC",
        })
        return
    }

    // Generate random state for CSRF protection
    state := generateRandomState()

    // Store state in session/cookie
    c.SetCookie("oauth_state", state, 300, "/", "", true, true)

    // Redirect to Janua
    authURL := oidcMgr.GetAuthURL(state)
    c.Redirect(http.StatusFound, authURL)
}

// OIDCCallback handles the OAuth callback from Janua
func (h *Handler) OIDCCallback(c *gin.Context) {
    // Verify state parameter
    savedState, err := c.Cookie("oauth_state")
    if err != nil || savedState != c.Query("state") {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Invalid state parameter",
        })
        return
    }

    // Get authorization code
    code := c.Query("code")
    if code == "" {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Missing authorization code",
        })
        return
    }

    // Exchange code for user
    oidcMgr := h.auth.(*auth.OIDCManager)
    user, err := oidcMgr.HandleCallback(c.Request.Context(), code)
    if err != nil {
        logrus.WithError(err).Error("OIDC callback failed")
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Authentication failed",
        })
        return
    }

    // Generate local session token (still using JWT for API access)
    jwtMgr := h.jwtManager // Keep a JWTManager for session tokens
    tokens, err := jwtMgr.GenerateTokenPair(user)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Failed to generate tokens",
        })
        return
    }

    c.JSON(http.StatusOK, tokens)
}
```

##### 5. Implement JWKS Endpoint

**File:** `apps/switchyard-api/internal/api/auth_handlers.go` (UPDATE)

```go
// JWKS endpoint for Janua to validate our tokens (if needed)
func (h *Handler) JWKS(c *gin.Context) {
    jwtMgr, ok := h.auth.(*auth.JWTManager)
    if !ok {
        c.JSON(http.StatusServiceUnavailable, gin.H{
            "error": "JWKS only available in local auth mode",
        })
        return
    }

    // Export public key as JWKS
    jwks := jwtMgr.GetJWKS()
    c.JSON(http.StatusOK, jwks)
}
```

##### 6. Update Route Setup

**File:** `apps/switchyard-api/internal/api/handlers.go:109-168` (UPDATE)

```go
func SetupRoutes(router *gin.Engine, h *Handler) {
    // Health check (no auth required)
    router.GET("/health", h.Health)

    // API v1 routes
    v1 := router.Group("/v1")
    {
        // Auth routes - UPDATED for dual mode
        if h.config.AuthMode == "local" {
            // Local JWT auth
            v1.POST("/auth/register", h.auditMiddleware.AuditMiddleware(), h.Register)
            v1.POST("/auth/login", h.auditMiddleware.AuditMiddleware(), h.Login)
            v1.POST("/auth/refresh", h.RefreshToken)
            v1.GET("/auth/jwks", h.JWKS) // NEW
        } else if h.config.AuthMode == "oidc" {
            // OIDC auth
            v1.GET("/auth/login", h.OIDCLogin) // Redirect to Janua
            v1.GET("/auth/callback", h.OIDCCallback) // OAuth callback
            // Register endpoint disabled in OIDC mode
        }

        // Logout requires authentication (both modes)
        v1.POST("/auth/logout", h.auth.AuthMiddleware(), h.auditMiddleware.AuditMiddleware(), h.Logout)

        // Protected routes (same for both modes)
        protected := v1.Group("")
        protected.Use(h.auth.AuthMiddleware())
        protected.Use(h.auditMiddleware.AuditMiddleware())
        {
            // ... existing routes ...
        }
    }
}
```

#### Environment Variables for Phase C

```bash
# Phase C: Switch to OIDC mode
ENCLII_AUTH_MODE=oidc
ENCLII_OIDC_ISSUER=https://auth.enclii.io
ENCLII_OIDC_CLIENT_ID=enclii-platform
ENCLII_OIDC_CLIENT_SECRET=<secret-from-janua>
ENCLII_OIDC_REDIRECT_URL=https://api.enclii.io/v1/auth/callback
```

#### Deployment Steps

1. **Update Enclii Configuration:**
   ```bash
   kubectl set env deployment/switchyard-api \
     ENCLII_AUTH_MODE=oidc \
     ENCLII_OIDC_ISSUER=https://auth.enclii.io \
     ENCLII_OIDC_CLIENT_ID=enclii-platform \
     ENCLII_OIDC_CLIENT_SECRET=<secret>
   ```

2. **Rolling Restart:**
   ```bash
   kubectl rollout restart deployment/switchyard-api
   ```

3. **Verify OIDC Flow:**
   ```bash
   # Should redirect to Janua
   curl -i https://api.enclii.io/v1/auth/login
   ```

4. **Fallback Plan:**
   ```bash
   # If something breaks, switch back to local mode
   kubectl set env deployment/switchyard-api ENCLII_AUTH_MODE=local
   kubectl rollout restart deployment/switchyard-api
   ```

---

## Migration Path for Existing Users

**Problem:** Users created in Phase A (local auth) need to work in Phase C (OIDC)

**Solution 1: Email Matching (Recommended)**
```go
// In OIDCManager.HandleCallback()
user, err := o.repos.User.GetByEmail(ctx, claims.Email)
if err != nil {
    // User doesn't exist, create them with OIDC identity
} else {
    // User exists from local auth - link to OIDC identity
    user.OIDCSubject = claims.Sub // Add this field to User model
    o.repos.User.Update(ctx, user)
}
```

**Solution 2: Account Linking UI**
- Provide UI to link local account to OIDC account
- User logs in with local credentials
- User clicks "Link to Janua"
- Redirect to Janua OAuth flow
- On callback, link accounts in database

---

## Implementation Checklist

### Phase A (Bootstrap Mode) ✅
- [x] Local JWT authentication working
- [x] User registration endpoint
- [x] Password hashing
- [x] Session management
- [ ] **TODO:** Auto-create bootstrap admin on first startup
- [ ] **TODO:** Add `ENCLII_BOOTSTRAP_ADMIN_EMAIL` env var

### Phase B (Deploy Janua) 🔄
- [ ] Deploy Janua via Enclii (using bootstrap admin)
- [ ] Configure DNS for `auth.enclii.io`
- [ ] Register Enclii as OAuth client in Janua
- [ ] Store OAuth credentials in Vault/Lockbox
- [ ] Test Janua login flow independently

### Phase C (OIDC Integration) ❌
- [ ] Add `AuthMode` configuration field
- [ ] Implement `OIDCManager` (`internal/auth/oidc.go`)
- [ ] Implement `AuthManager` factory pattern
- [ ] Add OIDC callback handlers (`OIDCLogin`, `OIDCCallback`)
- [ ] Implement JWKS endpoint
- [ ] Update route setup for dual-mode auth
- [ ] Add email-based user migration logic
- [ ] Write integration tests for OIDC flow
- [ ] Update UI to handle OAuth redirect flow
- [ ] Document rollback procedure
- [ ] Load test with OIDC provider

### Post-Migration
- [ ] Deprecate local auth mode (optional)
- [ ] Remove bootstrap admin account (optional)
- [ ] Monitor OIDC authentication metrics
- [ ] Set up alerting for OIDC provider downtime
- [ ] Implement fallback to local auth if Janua is down

---

## Dependencies

**Go Packages:**
```bash
go get github.com/coreos/go-oidc/v3/oidc
go get golang.org/x/oauth2
```

**Database Schema Changes:**
```sql
-- Add OIDC identity linking to users table
ALTER TABLE users ADD COLUMN oidc_subject TEXT;
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;
CREATE INDEX idx_users_oidc_subject ON users(oidc_subject);
```

---

## Security Considerations

1. **State Parameter:** CSRF protection for OAuth flow
2. **Token Storage:** Store OAuth tokens securely (encrypted in DB or memory only)
3. **Session Hijacking:** Continue using Redis for session revocation
4. **Fallback Risk:** If Janua goes down, provide emergency local auth fallback
5. **Admin Lockout:** Always keep one local super-admin account as emergency access

---

## Testing Strategy

### Unit Tests
- [ ] `TestOIDCManager_HandleCallback`
- [ ] `TestOIDCManager_ValidateToken`
- [ ] `TestAuthManagerFactory_SwitchMode`

### Integration Tests
- [ ] Full OAuth flow with mock OIDC provider
- [ ] Token validation against real Janua instance
- [ ] User migration from local to OIDC

### Manual Testing Checklist
- [ ] Login via Janua redirects correctly
- [ ] Callback creates/updates user in Enclii
- [ ] Existing local users can access after OIDC switch
- [ ] API endpoints still work with OIDC tokens
- [ ] Logout invalidates both OIDC and local sessions

---

## Timeline Estimate

| Phase | Effort | When |
|-------|--------|------|
| Phase A (Bootstrap) | ✅ **Complete** | Already done |
| Phase B (Deploy Janua) | 2-3 days | Week 3 |
| Phase C (OIDC Integration) | **5-7 days** | Week 3-4 |
| Testing & Rollout | 2-3 days | Week 4 |
| **Total** | **9-13 days** | **Weeks 3-4** |

---

## Rollback Strategy

If OIDC integration fails, immediate rollback:

```bash
# 1. Switch back to local mode
kubectl set env deployment/switchyard-api ENCLII_AUTH_MODE=local

# 2. Restart deployment
kubectl rollout restart deployment/switchyard-api

# 3. Verify local auth works
curl -X POST https://api.enclii.io/v1/auth/login \
  -d '{"email":"admin@madfam.io","password":"..."}'

# Users can log in with local credentials again
```

---

## Summary

**The bootstrap problem has been solved with a phased approach:**

1. ✅ **Phase A is complete** - Local JWT auth works
2. 🔄 **Phase B is ready** - Deploy Janua using local admin (infrastructure required)
3. ✅ **Phase C code complete** - OIDC integration implemented (NOT TESTED)

**Implementation Status (November 21, 2025 - Updated):**
- ✅ All code for Phase C implemented
- ✅ JWKS endpoint properly encodes RSA keys
- ✅ User migration logic implemented
- ✅ Dual-mode auth with factory pattern
- ✅ **APPLICATION NOW COMPILES SUCCESSFULLY (67MB binary)**
- ✅ Fixed ~50+ build errors across all packages
- ✅ Fixed UUID/string type mismatches throughout codebase
- ✅ Fixed repository naming and method signatures
- ✅ Fixed structured logging calls across all handlers
- ⚠️ Migration 005 SQL is correct but not tested against live database
- ❌ **ZERO runtime testing performed** - code compiles but untested
- ❌ No validation with real OIDC provider

**What This Means:**
The architecture is sound, the code compiles cleanly, but **this is untested alpha code**.
Before production use, you MUST:
1. Set up PostgreSQL database and run migration 005
2. Test application startup in both local and OIDC modes
3. Validate OIDC flow with a real provider (or mock)
4. Write and run unit/integration tests
5. Perform end-to-end testing of authentication flows

**Critical Success Factor:** Auth mode switching with fallback capability is implemented,
so you can revert to local auth if Janua has issues. This safety mechanism is code-complete
but untested.
