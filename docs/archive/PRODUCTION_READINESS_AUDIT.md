# Enclii Production Readiness Audit & Implementation Plan
**Date:** November 20, 2025 (Historical Planning Document)
**Scope:** Full codebase audit for Hetzner + Cloudflare deployment with Janua-as-a-Service
**Overall Production Readiness:** 95% (as of Jan 2026)

> ⚠️ **Historical Document Notice:**
> This audit was created in November 2025 as a planning document. **Actual current infrastructure (Jan 2026):**
> - Single Hetzner AX41-NVME dedicated server (~$50/mo)
> - Self-hosted PostgreSQL in-cluster (not Ubicloud)
> - Self-hosted Redis in-cluster (not Sentinel HA)
> - Single-node k3s with Longhorn (ready for multi-node scaling)
>
> See [Infrastructure Documentation](../infrastructure/README.md) for current state.

---

## Executive Summary

Enclii is **well-positioned for production deployment** with the new infrastructure stack. The codebase is cloud-agnostic, security-first, and requires only tactical updates for the Hetzner + Cloudflare stack. No architectural rewrites needed.

### Key Findings

| Component | Readiness | Status | Effort to 100% |
|-----------|-----------|--------|----------------|
| **Infrastructure Compatibility** | 75% | 🟡 Good | 1 week |
| **Janua Authentication** | 65% | 🟡 Moderate | 2-3 weeks |
| **Multi-Tenancy** | 70% | 🟡 Good | 1 week |
| **Database (Ubicloud)** | 95% | ✅ Excellent | 1 day |
| **Object Storage (R2)** | 40% | 🟠 Gap | 2 days |
| **Ingress (Cloudflare Tunnel)** | 60% | 🟠 Gap | 3 days |
| **Redis HA (Sentinel)** | 85% | 🟡 Good | 1 day |
| **Security** | 95% | ✅ Excellent | Ongoing |
| **Monitoring** | 50% | 🟠 Basic | 4 days |

### Cost Impact

**Current Monthly Cost (projected):** $100/month
- Hetzner: $45
- Ubicloud PostgreSQL: $50
- Cloudflare: $5 (R2 only)
- Redis Sentinel: $0 (included)
- Janua: $0 (shares infrastructure)

**5-Year Savings:** $125,000+ vs Railway + Auth0

---

## Part 1: Infrastructure Compatibility (75% Ready)

### ✅ What's Already Compatible

1. **No Vendor Lock-In**
   - Zero DigitalOcean-specific code found
   - No AWS/GCP-specific APIs
   - Standard Kubernetes manifests
   - Generic storage classes (`standard`)

2. **Database Configuration**
   - PgBouncer-compatible connection pooling
   - Proper timeout configurations
   - Health checks implemented
   - Migration system ready
   - **Verdict:** Drop-in compatible with Ubicloud PostgreSQL

3. **Build System**
   - No provider-specific CLI tools (doctl, aws, etc.)
   - Standard `kubectl` commands only
   - Works with any Kubernetes cluster
   - CI/CD uses Kind (provider-agnostic)

### ⚠️ Critical Gaps Identified

#### Gap 1: Cloudflare Tunnel Integration (HIGH PRIORITY)

**Current State:**
```
Internet → LoadBalancer (costs $) → nginx-ingress → Service
```

**Target State:**
```
Internet → Cloudflare Edge (FREE) → cloudflared → Service
```

**Files Requiring Changes:**

1. **`infra/k8s/base/ingress-nginx.yaml`** (DEPRECATE in production)
2. **`apps/switchyard-api/internal/reconciler/service.go`** (lines 625-730)
   - Make ingress generation environment-aware
   - Production: Skip Ingress, use ClusterIP only
   - Dev/Staging: Keep nginx-ingress

**Implementation:**

```yaml
# NEW: infra/k8s/production/cloudflared.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloudflared
  namespace: cloudflare-tunnel
spec:
  replicas: 3  # High availability
  selector:
    matchLabels:
      app: cloudflared
  template:
    metadata:
      labels:
        app: cloudflared
    spec:
      containers:
      - name: cloudflared
        image: cloudflare/cloudflared:latest
        args:
        - tunnel
        - --no-autoupdate
        - run
        - --credentials-file=/etc/cloudflared/credentials.json
        - enclii-production
        volumeMounts:
        - name: credentials
          mountPath: /etc/cloudflared
          readOnly: true
        livenessProbe:
          httpGet:
            path: /ready
            port: 2000
          initialDelaySeconds: 10
          periodSeconds: 10
      volumes:
      - name: credentials
        secret:
          secretName: cloudflared-credentials
---
apiVersion: v1
kind: Service
metadata:
  name: cloudflared
spec:
  selector:
    app: cloudflared
  ports:
  - port: 2000
    targetPort: 2000
    name: metrics
```

**Estimated Effort:** 3 days
**Cost Savings:** $90/year (LB) + $18/year (public IPs) = **$108/year**

---

#### Gap 2: Cloudflare R2 Object Storage (MEDIUM PRIORITY)

**Current State:**
- Backup code is S3-compatible ✅
- Build artifacts (SBOMs) generated but not persisted ❌
- No file upload handling ❌

**Implementation Required:**

**NEW FILE:** `apps/switchyard-api/internal/storage/r2.go`
```go
package storage

import (
	"context"
	"io"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

type R2Storage struct {
	client *s3.Client
	bucket string
}

func NewR2Storage(endpoint, bucket, accessKey, secretKey string) (*R2Storage, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("auto"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKey,
			secretKey,
			"",
		)),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:           endpoint,
					SigningRegion: "auto",
				}, nil
			},
		)),
	)
	if err != nil {
		return nil, err
	}

	return &R2Storage{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
	}, nil
}

func (r *R2Storage) Upload(ctx context.Context, key string, data io.Reader, contentType string) (string, error) {
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &r.bucket,
		Key:         &key,
		Body:        data,
		ContentType: &contentType,
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://%s/%s", r.bucket, key), nil
}

func (r *R2Storage) StoreSBOM(ctx context.Context, releaseID string, sbomData []byte) (string, error) {
	key := fmt.Sprintf("sboms/%s.json", releaseID)
	return r.Upload(ctx, key, bytes.NewReader(sbomData), "application/json")
}

func (r *R2Storage) GetSBOM(ctx context.Context, releaseID string) ([]byte, error) {
	key := fmt.Sprintf("sboms/%s.json", releaseID)
	result, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &r.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	return io.ReadAll(result.Body)
}
```

**Database Migration:**
```sql
-- NEW: apps/switchyard-api/internal/db/migrations/003_add_object_storage.up.sql
ALTER TABLE releases ADD COLUMN sbom_uri VARCHAR(500);
ALTER TABLE releases ADD COLUMN signature_uri VARCHAR(500);
ALTER TABLE releases ADD COLUMN artifact_uri VARCHAR(500);

CREATE INDEX idx_releases_sbom_uri ON releases(sbom_uri);
```

**Environment Variables:**
```bash
# Add to .env.example
ENCLII_R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
ENCLII_R2_BUCKET=enclii-production
ENCLII_R2_ACCESS_KEY_ID=<r2-access-key>
ENCLII_R2_SECRET_ACCESS_KEY=<r2-secret-key>
```

**Estimated Effort:** 2 days
**Cost Impact:** ~$5/month for 250GB storage

---

#### Gap 3: Redis Sentinel HA (MEDIUM PRIORITY)

**Current State:**
- Code is Sentinel-compatible ✅
- Deployment is single-node ❌

**Implementation Required:**

**NEW FILE:** `infra/k8s/production/redis-sentinel.yaml`
```yaml
---
# Redis Master
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis-master
  namespace: enclii-production
spec:
  serviceName: redis-master
  replicas: 1
  selector:
    matchLabels:
      app: redis
      role: master
  template:
    metadata:
      labels:
        app: redis
        role: master
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        ports:
        - containerPort: 6379
          name: redis
        command:
        - redis-server
        - --appendonly yes
        - --replica-announce-ip $(POD_IP)
        - --requirepass $(REDIS_PASSWORD)
        env:
        - name: POD_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        - name: REDIS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: redis-secret
              key: password
        volumeMounts:
        - name: data
          mountPath: /data
        livenessProbe:
          exec:
            command:
            - redis-cli
            - --pass
            - $(REDIS_PASSWORD)
            - ping
          initialDelaySeconds: 30
          periodSeconds: 10
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 5Gi
---
# Redis Replicas
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis-replica
  namespace: enclii-production
spec:
  serviceName: redis-replica
  replicas: 2
  selector:
    matchLabels:
      app: redis
      role: replica
  template:
    metadata:
      labels:
        app: redis
        role: replica
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        ports:
        - containerPort: 6379
          name: redis
        command:
        - redis-server
        - --appendonly yes
        - --replicaof redis-master-0.redis-master 6379
        - --replica-announce-ip $(POD_IP)
        - --requirepass $(REDIS_PASSWORD)
        - --masterauth $(REDIS_PASSWORD)
        env:
        - name: POD_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        - name: REDIS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: redis-secret
              key: password
        volumeMounts:
        - name: data
          mountPath: /data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 5Gi
---
# Redis Sentinel
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis-sentinel
  namespace: enclii-production
spec:
  serviceName: redis-sentinel
  replicas: 3
  selector:
    matchLabels:
      app: redis-sentinel
  template:
    metadata:
      labels:
        app: redis-sentinel
    spec:
      containers:
      - name: sentinel
        image: redis:7-alpine
        ports:
        - containerPort: 26379
          name: sentinel
        command:
        - redis-sentinel
        - /etc/redis/sentinel.conf
        volumeMounts:
        - name: config
          mountPath: /etc/redis
      volumes:
      - name: config
        configMap:
          name: redis-sentinel-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: redis-sentinel-config
  namespace: enclii-production
data:
  sentinel.conf: |
    port 26379
    sentinel monitor mymaster redis-master-0.redis-master 6379 2
    sentinel down-after-milliseconds mymaster 5000
    sentinel failover-timeout mymaster 10000
    sentinel parallel-syncs mymaster 1
---
apiVersion: v1
kind: Service
metadata:
  name: redis-sentinel
  namespace: enclii-production
spec:
  clusterIP: None
  ports:
  - port: 26379
    targetPort: 26379
    name: sentinel
  selector:
    app: redis-sentinel
```

**Code Update:** `apps/switchyard-api/internal/cache/redis.go`
```go
func NewRedisCache(config *CacheConfig) (*RedisCache, error) {
	var rdb *redis.Client

	// Check if Sentinel mode is enabled
	if config.SentinelMaster != "" && len(config.SentinelAddrs) > 0 {
		rdb = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    config.SentinelMaster,
			SentinelAddrs: config.SentinelAddrs,
			Password:      config.Password,
			DB:            0,
		})
	} else {
		// Fallback to standalone mode
		rdb = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
			Password: config.Password,
			DB:       0,
		})
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisCache{client: rdb}, nil
}
```

**Estimated Effort:** 1 day
**Failover Time:** 10-20 seconds (automatic)

---

## Part 2: Janua Integration (65% Ready)

### Current Auth System Analysis

**Strengths:**
- ✅ Already uses RS256 JWT (perfect for Janua!)
- ✅ Database schema has `oidc_sub` field (OIDC-ready)
- ✅ Session management with Redis
- ✅ RBAC with roles and project access
- ✅ Audit logging

**Critical Gap:**
- ⚠️ Conflicting auth implementations:
  - `internal/auth/jwt.go` - RS256 (production-ready, 472 lines)
  - `internal/middleware/auth.go` - HS256 (legacy, 216 lines)
  - **ACTION:** Deprecate middleware, use JWTManager everywhere

### Janua Integration Roadmap

#### Phase 3A: Backend Janua Integration (Week 1)

**Step 1: Deploy Janua**

**NEW FILE:** `infra/k8s/base/janua.yaml`
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: janua
  namespace: enclii-production
spec:
  replicas: 3
  selector:
    matchLabels:
      app: janua
  template:
    metadata:
      labels:
        app: janua
    spec:
      containers:
      - name: janua
        image: ghcr.io/madfam-org/janua:latest
        ports:
        - containerPort: 8000
          name: http
        env:
        # Database (Shared Ubicloud PostgreSQL)
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: janua-secrets
              key: database-url
        # Redis (Shared Sentinel)
        - name: REDIS_URL
          value: "redis://redis-sentinel:26379/1?master=mymaster"
        # JWT Configuration
        - name: JWT_ALGORITHM
          value: "RS256"
        - name: JWT_PRIVATE_KEY
          valueFrom:
            secretKeyRef:
              name: janua-secrets
              key: jwt-private-key
        - name: JWT_PUBLIC_KEY
          valueFrom:
            secretKeyRef:
              name: janua-secrets
              key: jwt-public-key
        # OAuth Configuration
        - name: JANUA_BASE_URL
          value: "https://auth.enclii.dev"
        - name: JANUA_ALLOWED_ORIGINS
          value: "https://app.enclii.dev,https://enclii.dev"
        # Feature Flags
        - name: JANUA_ENABLE_SAML
          value: "true"
        - name: JANUA_ENABLE_MFA
          value: "true"
        resources:
          requests:
            cpu: 200m
            memory: 256Mi
          limits:
            cpu: 1000m
            memory: 1Gi
        livenessProbe:
          httpGet:
            path: /health
            port: 8000
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 8000
          initialDelaySeconds: 10
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: janua
  namespace: enclii-production
spec:
  selector:
    app: janua
  ports:
  - port: 8000
    targetPort: 8000
    name: http
  type: ClusterIP
```

**Step 2: JWKS Provider**

**NEW FILE:** `apps/switchyard-api/internal/auth/jwks_provider.go`
```go
package auth

import (
	"context"
	"fmt"
	"time"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

type JWKSProvider struct {
	jwksURL string
	cache   *jwk.Cache
}

func NewJWKSProvider(jwksURL string) (*JWKSProvider, error) {
	// Create JWKS cache with 15-minute refresh interval
	cache := jwk.NewCache(context.Background())

	// Register JWKS endpoint
	if err := cache.Register(jwksURL, jwk.WithMinRefreshInterval(15*time.Minute)); err != nil {
		return nil, fmt.Errorf("failed to register JWKS URL: %w", err)
	}

	// Trigger initial fetch
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("failed to fetch initial JWKS: %w", err)
	}

	return &JWKSProvider{
		jwksURL: jwksURL,
		cache:   cache,
	}, nil
}

func (p *JWKSProvider) GetKeySet(ctx context.Context) (jwk.Set, error) {
	return p.cache.Get(ctx, p.jwksURL)
}

func (p *JWKSProvider) GetPublicKey(ctx context.Context, kid string) (interface{}, error) {
	keyset, err := p.GetKeySet(ctx)
	if err != nil {
		return nil, err
	}

	key, ok := keyset.LookupKeyID(kid)
	if !ok {
		return nil, fmt.Errorf("key with ID %s not found", kid)
	}

	var pubkey interface{}
	if err := key.Raw(&pubkey); err != nil {
		return nil, fmt.Errorf("failed to get raw key: %w", err)
	}

	return pubkey, nil
}
```

**Step 3: OAuth Handlers**

**NEW FILE:** `apps/switchyard-api/internal/api/oauth_handlers.go`
```go
package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"
	"github.com/gin-gonic/gin"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OAuthHandler struct {
	config       *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	authService  *services.AuthService
	stateStore   map[string]string // TODO: Use Redis
}

func NewOAuthHandler(
	clientID, clientSecret, issuerURL, redirectURL string,
	authService *services.AuthService,
) (*OAuthHandler, error) {
	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	return &OAuthHandler{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier:    provider.Verifier(&oidc.Config{ClientID: clientID}),
		authService: authService,
		stateStore:  make(map[string]string),
	}, nil
}

func (h *OAuthHandler) InitiateLogin(c *gin.Context) {
	// Generate random state
	b := make([]byte, 32)
	rand.Read(b)
	state := base64.URLEncoding.EncodeToString(b)

	// Store state (TODO: Use Redis with TTL)
	h.stateStore[state] = time.Now().Format(time.RFC3339)

	// Redirect to Janua
	authURL := h.config.AuthCodeURL(state)
	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

func (h *OAuthHandler) HandleCallback(c *gin.Context) {
	// Verify state
	state := c.Query("state")
	if _, ok := h.stateStore[state]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	delete(h.stateStore, state)

	// Exchange code for tokens
	code := c.Query("code")
	ctx := c.Request.Context()
	oauth2Token, err := h.config.Exchange(ctx, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to exchange code"})
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no id_token in response"})
		return
	}

	idToken, err := h.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify ID token"})
		return
	}

	// Extract claims
	var claims struct {
		Email         string `json:"email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse claims"})
		return
	}

	// Find or create user
	user, err := h.authService.FindOrCreateOIDCUser(ctx, idToken.Subject, claims.Email, claims.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	// Generate Enclii session tokens
	accessToken, refreshToken, err := h.authService.GenerateTokensForUser(ctx, user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tokens"})
		return
	}

	// Return tokens (or set cookies)
	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"user":          user,
	})
}
```

**Step 4: Update Auth Service**

**UPDATE:** `apps/switchyard-api/internal/services/auth.go`
```go
// Add OIDC user creation
func (s *AuthService) FindOrCreateOIDCUser(ctx context.Context, oidcSub, email, name string) (*types.User, error) {
	// Try to find existing user by OIDC sub
	user, err := s.userRepo.GetByOIDCSub(ctx, oidcSub)
	if err == nil {
		// User exists, update last login
		user.LastLoginAt = time.Now()
		if err := s.userRepo.Update(ctx, user); err != nil {
			return nil, err
		}
		return user, nil
	}

	// Try to find by email (for migration case)
	user, err = s.userRepo.GetByEmail(ctx, email)
	if err == nil {
		// Link OIDC to existing account
		user.OIDCSub = &oidcSub
		user.LastLoginAt = time.Now()
		if err := s.userRepo.Update(ctx, user); err != nil {
			return nil, err
		}

		// Audit log
		s.auditLogger.Log(ctx, &types.AuditLog{
			Action:    "user.oidc_linked",
			UserID:    user.ID,
			Resource:  "user",
			Metadata:  map[string]interface{}{"email": email},
		})

		return user, nil
	}

	// Create new user
	newUser := &types.User{
		Email:    email,
		Name:     name,
		OIDCSub:  &oidcSub,
		Active:   true,
		CreatedAt: time.Now(),
		LastLoginAt: time.Now(),
	}

	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, err
	}

	// Audit log
	s.auditLogger.Log(ctx, &types.AuditLog{
		Action:   "user.created_oidc",
		UserID:   newUser.ID,
		Resource: "user",
		Metadata: map[string]interface{}{"email": email},
	})

	return newUser, nil
}
```

**Estimated Effort:** 5-7 days

---

#### Phase 3B: Frontend Janua Integration (3-4 days)

**Step 1: Install Dependencies**

```bash
cd apps/switchyard-ui
npm install oidc-client-ts@^2.4.0
```

**Step 2: Replace AuthContext**

**UPDATE:** `apps/switchyard-ui/contexts/AuthContext.tsx`
```typescript
'use client';

import React, { createContext, useContext, useEffect, useState } from 'react';
import { UserManager, User, UserManagerSettings } from 'oidc-client-ts';

interface AuthContextType {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  login: () => void;
  logout: () => void;
  signinCallback: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

const oidcConfig: UserManagerSettings = {
  authority: process.env.NEXT_PUBLIC_JANUA_ISSUER || 'https://auth.enclii.dev',
  client_id: process.env.NEXT_PUBLIC_JANUA_CLIENT_ID || 'enclii-web',
  redirect_uri: `${window.location.origin}/auth/callback`,
  post_logout_redirect_uri: `${window.location.origin}/`,
  response_type: 'code',
  scope: 'openid profile email',
  automaticSilentRenew: true,
  loadUserInfo: true,
};

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [userManager] = useState(() => new UserManager(oidcConfig));
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    // Load user on mount
    userManager.getUser().then((user) => {
      setUser(user);
      setIsLoading(false);
    });

    // Listen for token events
    userManager.events.addUserLoaded((user) => setUser(user));
    userManager.events.addUserUnloaded(() => setUser(null));
    userManager.events.addAccessTokenExpired(() => {
      userManager.signinSilent().catch(() => setUser(null));
    });
  }, [userManager]);

  const login = () => {
    userManager.signinRedirect();
  };

  const logout = () => {
    userManager.signoutRedirect();
  };

  const signinCallback = async () => {
    try {
      const user = await userManager.signinRedirectCallback();
      setUser(user);
    } catch (error) {
      console.error('OAuth callback error:', error);
      throw error;
    }
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        isAuthenticated: !!user && !user.expired,
        isLoading,
        login,
        logout,
        signinCallback,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider');
  }
  return context;
}

export function useRequireAuth() {
  const { isAuthenticated, isLoading } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (!isLoading && !isAuthenticated) {
      router.push('/login');
    }
  }, [isAuthenticated, isLoading, router]);

  return { isAuthenticated, isLoading };
}
```

**Step 3: Create Callback Page**

**NEW FILE:** `apps/switchyard-ui/app/auth/callback/page.tsx`
```typescript
'use client';

import { useEffect } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import { useRouter } from 'next/navigation';

export default function AuthCallbackPage() {
  const { signinCallback } = useAuth();
  const router = useRouter();

  useEffect(() => {
    signinCallback()
      .then(() => router.push('/dashboard'))
      .catch((error) => {
        console.error('Callback error:', error);
        router.push('/login?error=callback_failed');
      });
  }, []);

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="text-center">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-gray-900 mx-auto" />
        <p className="mt-4 text-gray-600">Completing sign-in...</p>
      </div>
    </div>
  );
}
```

**Step 4: Update Login Page**

**UPDATE:** `apps/switchyard-ui/app/login/page.tsx`
```typescript
'use client';

import { useAuth } from '@/contexts/AuthContext';
import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

export default function LoginPage() {
  const { login, isAuthenticated } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (isAuthenticated) {
      router.push('/dashboard');
    }
  }, [isAuthenticated, router]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50">
      <div className="max-w-md w-full space-y-8 p-8 bg-white rounded-lg shadow">
        <div className="text-center">
          <h2 className="text-3xl font-bold">Sign in to Enclii</h2>
          <p className="mt-2 text-gray-600">Deploy with confidence</p>
        </div>
        <button
          onClick={login}
          className="w-full flex justify-center py-3 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
        >
          Sign in with Janua
        </button>
      </div>
    </div>
  );
}
```

**Estimated Effort:** 3-4 days

---

## Part 3: Multi-Tenant Janua-as-a-Service Architecture

### Option 1: Shared Janua (Recommended for MVP)

**Architecture:**
```
┌─────────────────────────────────────────────┐
│   Single Janua Instance (3 replicas)       │
│   - Uses Janua's native org multi-tenancy │
│   - All customers share one Janua          │
│   - Cost: $50/mo (PostgreSQL only)          │
└─────────────────────────────────────────────┘
         ▲              ▲              ▲
         │              │              │
    Customer A     Customer B     Customer C
    (Org ID 1)     (Org ID 2)     (Org ID 3)
```

**Implementation:**

```go
// When provisioning Janua for a customer project
func (s *ProjectService) EnableJanuaAuth(ctx context.Context, projectID uuid.UUID) error {
	// 1. Create organization in shared Janua
	org, err := s.januaClient.CreateOrganization(ctx, &janua.Organization{
		Name:        project.Name,
		Slug:        project.Slug,
		ExternalID:  projectID.String(),  // Link to Enclii project
	})
	if err != nil {
		return err
	}

	// 2. Create OAuth client for this organization
	client, err := s.januaClient.CreateOAuthClient(ctx, org.ID, &janua.OAuthClient{
		Name:         fmt.Sprintf("%s App", project.Name),
		RedirectURIs: []string{
			fmt.Sprintf("https://%s.enclii.dev/auth/callback", project.Slug),
		},
		AllowedScopes: []string{"openid", "profile", "email"},
	})
	if err != nil {
		return err
	}

	// 3. Store Janua configuration in Enclii database
	authConfig := &types.AuthConfig{
		ProjectID:    projectID,
		Provider:     "janua",
		Issuer:       "https://auth.enclii.dev",
		ClientID:     client.ID,
		ClientSecret: client.Secret,
		OrganizationID: org.ID,
	}

	return s.authConfigRepo.Create(ctx, authConfig)
}
```

**Pros:**
- ✅ Simple operations (one Janua instance)
- ✅ Cost-effective (~$50/mo total for unlimited customers)
- ✅ Janua natively supports multi-tenancy

**Cons:**
- ⚠️ Shared blast radius (one Janua failure affects all customers)
- ⚠️ Tenant isolation via application logic only

---

### Option 2: Janua-per-Customer (Enterprise)

**Architecture:**
```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Janua-Cust1 │  │ Janua-Cust2 │  │ Janua-Cust3 │
│ + PostgreSQL │  │ + PostgreSQL │  │ + PostgreSQL │
│ + Redis      │  │ + Redis      │  │ + Redis      │
│ Cost: $100/mo│  │ Cost: $100/mo│  │ Cost: $100/mo│
└──────────────┘  └──────────────┘  └──────────────┘
```

**Implementation:**

```go
// Dynamically provision Janua instance for enterprise customer
func (s *ProjectService) ProvisionDedicatedJanua(ctx context.Context, projectID uuid.UUID) error {
	project, _ := s.projectRepo.Get(ctx, projectID)
	namespace := fmt.Sprintf("janua-%s", project.Slug)

	// 1. Create namespace
	if err := s.k8sClient.CreateNamespace(ctx, namespace); err != nil {
		return err
	}

	// 2. Apply Janua Helm chart
	helmValues := map[string]interface{}{
		"ingress": map[string]interface{}{
			"host": fmt.Sprintf("auth-%s.enclii.dev", project.Slug),
		},
		"database": map[string]interface{}{
			"host": fmt.Sprintf("postgres-%s.enclii.dev", project.Slug),
		},
	}

	if err := s.helmClient.Install(ctx, "janua", namespace, helmValues); err != nil {
		return err
	}

	// 3. Store dedicated Janua URL
	authConfig := &types.AuthConfig{
		ProjectID:    projectID,
		Provider:     "janua-dedicated",
		Issuer:       fmt.Sprintf("https://auth-%s.enclii.dev", project.Slug),
		Dedicated:    true,
	}

	return s.authConfigRepo.Create(ctx, authConfig)
}
```

**Pros:**
- ✅ Strong tenant isolation
- ✅ Compliance-friendly (data residency)
- ✅ Customizable per-tenant (versions, configs)

**Cons:**
- ⚠️ High complexity (N Janua instances to manage)
- ⚠️ Cost scales linearly (~$100/mo per tenant)
- ⚠️ Requires automation for provisioning

**Recommendation:** Start with Option 1 (Shared), build Option 2 for enterprise tier

---

## Part 4: Implementation Priorities

### Week 1-2: Production MVP (Infrastructure)

**Goal:** Deploy to Hetzner with Cloudflare integration

| Day | Task | Estimated Hours | Blockers |
|-----|------|-----------------|----------|
| 1-2 | Cloudflare Tunnel setup | 12h | None |
| 3 | Redis Sentinel deployment | 8h | None |
| 4 | Cloudflare R2 implementation | 8h | None |
| 5 | Ubicloud PostgreSQL migration | 6h | Need Ubicloud account |
| 6-7 | Production deployment & testing | 12h | All above complete |

**Total:** 46 hours (~1.5 weeks)

**Deliverables:**
- ✅ Enclii running on Hetzner
- ✅ Cloudflare Tunnel ingress
- ✅ Redis HA with Sentinel
- ✅ R2 object storage for SBOMs
- ✅ Ubicloud managed PostgreSQL

---

### Week 3-4: Janua Integration (Authentication)

**Goal:** Replace custom auth with Janua OAuth

| Day | Task | Estimated Hours | Blockers |
|-----|------|-----------------|----------|
| 8-9 | Deploy Janua to cluster | 12h | Week 1-2 complete |
| 10-11 | JWKS provider + OAuth handlers | 16h | Janua deployed |
| 12-13 | Frontend OAuth integration | 12h | Backend ready |
| 14 | User migration tooling | 8h | Frontend complete |

**Total:** 48 hours (~1.5 weeks)

**Deliverables:**
- ✅ Janua deployed and operational
- ✅ OAuth login flow working
- ✅ Existing users migrated
- ✅ Dual auth support (temporary)

---

### Week 5-6: Multi-Tenancy & Polish

**Goal:** Production-ready multi-tenant platform

| Day | Task | Estimated Hours | Blockers |
|-----|------|-----------------|----------|
| 15-16 | Cloudflare for SaaS integration | 12h | None |
| 17-18 | ResourceQuotas per tenant | 8h | None |
| 19-20 | Janua-as-a-Service provisioning | 16h | Janua integration complete |
| 21-22 | Monitoring (Prometheus/Grafana) | 12h | None |
| 23-24 | Load testing & optimization | 12h | All features complete |

**Total:** 60 hours (2 weeks)

**Deliverables:**
- ✅ Multi-tenant Janua architecture
- ✅ Customer custom domains with auto-SSL
- ✅ Resource isolation per tenant
- ✅ Production monitoring
- ✅ Load tested to 1000 RPS

---

## Part 5: Cost & ROI Analysis

### Infrastructure Costs (Monthly)

| Component | Option | Cost |
|-----------|--------|------|
| **Compute** | Hetzner 3x CPX31 | $45 |
| **Database** | Ubicloud PostgreSQL HA | $50 |
| **Object Storage** | Cloudflare R2 | $5 |
| **Ingress** | Cloudflare Tunnel | $0 |
| **Custom Domains** | Cloudflare for SaaS (100 free) | $0 |
| **Redis** | Self-hosted Sentinel | $0 |
| **Auth (Janua)** | Self-hosted (shared infra) | $0 |
| **Monitoring** | Self-hosted Prometheus/Grafana | $0 |
| **Total** | | **$100/month** |

**Staging Environment:** +$50/month (50% of production)
**Grand Total:** **$150/month**

### Comparison vs Alternatives

| Solution | Monthly | Annual | 5-Year |
|----------|---------|--------|--------|
| **Enclii (Hetzner + Cloudflare + Janua)** | $100 | $1,200 | $6,000 |
| **Railway + Auth0** | $2,220 | $26,640 | $133,200 |
| **Vercel + Clerk** | $2,500 | $30,000 | $150,000 |
| **DigitalOcean (managed)** | $341 | $4,092 | $20,460 |

### ROI Calculation

**5-Year Savings:**
- vs Railway + Auth0: **$127,200** (2,122% ROI)
- vs Vercel + Clerk: **$144,000** (2,400% ROI)
- vs DigitalOcean: **$14,460** (241% ROI)

**Time Investment:**
- Infrastructure setup: 46 hours (~$9,200 at $200/hr)
- Janua integration: 48 hours (~$9,600 at $200/hr)
- Total investment: **~$19,000**

**Payback Period:**
- vs Railway + Auth0: **0.9 months**
- vs DigitalOcean: **7.9 months**

---

## Part 6: Risk Assessment

### Technical Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| **Hetzner outage** | Low | High | Multi-region ready, can migrate to any Kubernetes |
| **Cloudflare Tunnel failure** | Low | High | 3 replicas for HA, automatic failover |
| **Janua production bugs** | Medium | Medium | Shared infra reduces blast radius, version pinning |
| **Database failover delay** | Low | Medium | Ubicloud automated failover (<30s) |
| **R2 API changes** | Low | Low | S3-compatible, easy to switch to Wasabi/Backblaze |

### Operational Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| **Complex multi-tenant auth** | Medium | Medium | Start with shared Janua (simpler) |
| **Insufficient monitoring** | High | High | Deploy Prometheus/Grafana in Week 5-6 |
| **Key rotation failure** | Low | High | JWKS automatic refresh, alerts on failures |
| **Backup restoration untested** | High | Critical | Weekly DR drills, automated testing |

---

## Part 7: Success Criteria

### Production Readiness Checklist (Must Pass)

- [ ] Infrastructure deployed to Hetzner
- [ ] Cloudflare Tunnel operational (3 replicas)
- [ ] Ubicloud PostgreSQL HA (Primary + Standby)
- [ ] Redis Sentinel HA (3 sentinels, quorum 2)
- [ ] Cloudflare R2 storing SBOMs and backups
- [ ] Janua deployed and operational
- [ ] OAuth login flow working
- [ ] At least one test application deployed
- [ ] TLS working for all domains
- [ ] Monitoring dashboards operational
- [ ] Load tested to 1000 RPS with no errors
- [ ] Backup restoration tested successfully
- [ ] Disaster recovery runbook documented

### Key Metrics (30 Days Post-Launch)

| Metric | Target | Measurement |
|--------|--------|-------------|
| **Uptime SLA** | 99.95% | Prometheus uptime checks |
| **P95 API Latency** | <200ms | Grafana dashboard |
| **Error Rate** | <0.1% | Application logs |
| **Auth Flow Success** | >99% | Janua metrics |
| **Infrastructure Cost** | <$120/mo | Billing reports |
| **SBOM Compliance** | 100% | All releases have SBOMs in R2 |
| **Custom Domain Provisioning** | <60s | Cloudflare for SaaS API |

---

## Conclusion

Enclii is **70% production-ready** with the new infrastructure stack. The codebase is well-architected, security-first, and requires only **6-8 weeks** of focused development to reach 95%+ readiness.

**Key Strengths:**
- ✅ Cloud-agnostic Kubernetes manifests
- ✅ Already using RS256 JWT (Janua-compatible!)
- ✅ OIDC-ready database schema
- ✅ Multi-tenant namespace architecture
- ✅ Excellent security posture
- ✅ No vendor lock-in

**Critical Path:**
1. **Week 1-2:** Infrastructure (Cloudflare Tunnel, R2, Redis Sentinel, Ubicloud)
2. **Week 3-4:** Janua integration (OAuth, JWKS, user migration)
3. **Week 5-6:** Polish (multi-tenancy, monitoring, load testing)

**Financial Impact:**
- **Monthly cost:** $100 (vs $2,220 with Railway + Auth0)
- **5-year savings:** $127,200+
- **Payback period:** <1 month

**Recommendation:** Proceed with implementation. The technical foundation is solid, the cost savings are massive, and the timeline is achievable.

---

**Next Step:** Begin Week 1 infrastructure deployment on Hetzner + Cloudflare.
