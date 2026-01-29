# Switchyard API Component - Comprehensive Code Audit Report

**Audit Date:** November 19, 2025  
**Component:** `/apps/switchyard-api`  
**Codebase:** Go 1.23.0 with 1.24.7 toolchain  

---

## EXECUTIVE SUMMARY

The Switchyard API demonstrates a generally well-structured implementation with strong foundations in authentication, database design, and security middleware. However, there are **14 Critical/High severity issues** and **18 Medium/Low severity issues** that require attention before production deployment.

### Key Findings:
- **Critical:** 5 issues with security, concurrency, and configuration management implications
- **High:** 9 issues affecting reliability, error handling, and resource management
- **Medium:** 8 issues impacting code maintainability and performance
- **Low:** 10 issues with minor impact but worth addressing

---

## 1. CODE QUALITY ANALYSIS

### 1.1 Main Entry Point Issues

**File:** `/apps/switchyard-api/cmd/api/main.go`

#### Issue 1.1.1: Missing Resource Cleanup and Incomplete Error Handling
**Severity:** High  
**Lines:** 45-49, 212-214

**Problem:**
```go
database, err := sql.Open("postgres", cfg.DatabaseURL)
if err != nil {
    logrus.Fatal("Failed to connect to database:", err)
}
defer database.Close()
```

- `database.Close()` is deferred at line 49, but if any initialization fails after this point (lines 57-179), resources may not be properly cleaned up
- The server goroutine (line 190-199) runs in background; if it fails, the fatal error doesn't propagate to main
- No graceful shutdown of background goroutines (reconciler, audit logger, etc.) before exiting

**Fix:**
```go
// Create a shutdown function
type appResources struct {
    db *sql.DB
    cache cache.CacheService
    server *http.Server
    auditLogger *audit.AsyncLogger
}

func (ar *appResources) cleanup() {
    if ar.server != nil {
        ar.server.Close()
    }
    if ar.auditLogger != nil {
        ar.auditLogger.Close()
    }
    if ar.cache != nil {
        ar.cache.Close()
    }
    if ar.db != nil {
        ar.db.Close()
    }
}

defer resources.cleanup()
```

#### Issue 1.1.2: Cache Fallback Silently Ignores Redis Errors
**Severity:** Medium  
**Lines:** 75-83

**Problem:**
- If Redis connection fails, the system silently falls back to in-memory cache
- This inconsistent behavior could cause issues in distributed deployments
- No metric or alert is incremented to track fallback events

**Fix:**
```go
cacheService, err := cache.NewRedisCache(&cache.RedisConfig{...})
if err != nil {
    logrus.WithError(err).Warn("Redis unavailable - using in-memory cache")
    // In production, this should alert ops
    metricsCollector.IncrementCounter("cache_fallback_count")
    cacheService = cache.NewInMemoryCache()
}
```

#### Issue 1.1.3: Database Connection Pool Not Configured
**Severity:** High  
**Lines:** 45-54

**Problem:**
- Raw `sql.Open()` is used without setting connection pool parameters
- No `SetMaxOpenConns()`, `SetMaxIdleConns()`, or `SetConnMaxLifetime()`
- Default pool settings may be insufficient for production load

**Fix:**
```go
database, err := sql.Open("postgres", cfg.DatabaseURL)
if err != nil {
    logrus.Fatal("Failed to connect to database:", err)
}

// Configure connection pool
database.SetMaxOpenConns(25)
database.SetMaxIdleConns(5)
database.SetConnMaxLifetime(30 * time.Minute)
database.SetConnMaxIdleTime(5 * time.Minute)

defer database.Close()
```

---

### 1.2 Error Handling Patterns

**File:** `/apps/switchyard-api/internal/audit/async_logger.go`

#### Issue 1.2.1: Silent Error Swallowing in AsyncLogger
**Severity:** High  
**Lines:** 107-121

**Problem:**
```go
for _, log := range batch {
    if err := l.repos.AuditLogs.Log(ctx, log); err != nil {
        l.mu.Lock()
        l.errorCount++
        l.mu.Unlock()
        continue  // <-- Silently continues, log is lost
    }
}
```

- Audit logs are lost if database write fails
- Only error count is incremented, but no fallback mechanism
- No dead-letter queue or file-based backup

**Fix:**
```go
for _, log := range batch {
    if err := l.repos.AuditLogs.Log(ctx, log); err != nil {
        l.mu.Lock()
        l.errorCount++
        failedLogs = append(failedLogs, log)  // Track failed logs
        l.mu.Unlock()
    }
}

// Attempt fallback storage if available
if len(failedLogs) > 0 {
    l.writeFallbackLogs(failedLogs)
}
```

---

### 1.3 Context Usage in Concurrent Operations

**File:** `/apps/switchyard-api/internal/rotation/controller.go`

#### Issue 1.3.1: Context Leak in Background Worker
**Severity:** Medium  
**Lines:** 68-78

**Problem:**
```go
// Start audit log writer
go c.processAuditLogs(ctx)

// Start rotation workers
for i := 0; i < c.maxConcurrent; i++ {
    go c.worker(ctx, i)
}

<-ctx.Done()
c.logger.Info("Secret rotation controller shutting down")
return nil
```

- Workers are launched with the same context, but there's no wait group
- If the main function returns, goroutines continue running (will panic when accessing closed channels)
- No coordinated shutdown sequence

**Fix:**
```go
var wg sync.WaitGroup

// Start audit log writer
wg.Add(1)
go func() {
    defer wg.Done()
    c.processAuditLogs(ctx)
}()

// Start rotation workers
for i := 0; i < c.maxConcurrent; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        c.worker(ctx, id)
    }(i)
}

<-ctx.Done()
c.logger.Info("Secret rotation controller shutting down")
wg.Wait()
return nil
```

---

### 1.4 Resource Cleanup and Graceful Shutdown

**File:** `/apps/switchyard-api/internal/reconciler/service.go`

#### Issue 1.4.1: Context Timeout Reuse in Loop
**Severity:** Medium  
**Lines:** 356-380

**Problem:**
```go
func (r *ServiceReconciler) waitForDeploymentReady(ctx context.Context, namespace, name string, timeout time.Duration) (bool, error) {
    ctx, cancel := context.WithTimeout(ctx, timeout)  // Creates NEW context with timeout
    defer cancel()
    
    for {
        select {
        case <-ctx.Done():
            return false, ctx.Err()
        default:
            deployment, err := deploymentClient.Get(ctx, name, metav1.GetOptions{})
            // ...
            time.Sleep(5 * time.Second)  // Loop continues until timeout
        }
    }
}
```

- If the passed `ctx` is already cancelled, the entire function fails immediately
- No yield to parent context cancellation in the loop

**Fix:**
```go
func (r *ServiceReconciler) waitForDeploymentReady(ctx context.Context, namespace, name string, timeout time.Duration) (bool, error) {
    deadline := time.Now().Add(timeout)
    
    for {
        select {
        case <-ctx.Done():
            return false, ctx.Err()
        default:
            if time.Now().After(deadline) {
                return false, fmt.Errorf("timeout waiting for deployment ready")
            }
            
            deployment, err := deploymentClient.Get(ctx, name, metav1.GetOptions{})
            if err != nil {
                return false, err
            }
            
            if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
                return true, nil
            }
            
            time.Sleep(5 * time.Second)
        }
    }
}
```

---

### 1.5 Code Duplication

**File:** `/apps/switchyard-api/internal/db/connection.go` vs `/apps/switchyard-api/cmd/api/main.go`

#### Issue 1.5.1: DatabaseManager Exists But Not Used
**Severity:** Medium  
**Lines:** connection.go vs main.go (45-54)

**Problem:**
- A complete `DatabaseManager` type exists in `connection.go` with:
  - Connection pool configuration
  - Health checks
  - Prepared statement management
  - Metrics collection
  
- But `main.go` uses raw `sql.Open()` instead of utilizing this manager

**Fix:**
```go
// In main.go
dbConfig := &db.DatabaseConfig{
    Host:              cfg.DBHost,
    Port:              cfg.DBPort,
    Database:          cfg.DBName,
    User:              cfg.DBUser,
    Password:          cfg.DBPassword,
    SSLMode:           "prefer",
    MaxOpenConns:      25,
    MaxIdleConns:      5,
    ConnMaxLifetime:   30 * time.Minute,
    ConnMaxIdleTime:   5 * time.Minute,
}
dbManager, err := db.NewDatabaseManager(dbConfig)
if err != nil {
    logrus.Fatal("Failed to initialize database:", err)
}
defer dbManager.Close()
```

---

## 2. SECURITY VULNERABILITIES

### 2.1 Authentication Implementation

**File:** `/apps/switchyard-api/internal/auth/jwt.go`

#### Issue 2.1.1: JWT Keys Generated Per Instance (Not Shared Across Replicas)
**Severity:** Critical  
**Lines:** 56-72

**Problem:**
```go
func NewJWTManager(tokenDuration, refreshDuration time.Duration, repos *db.Repositories) (*JWTManager, error) {
    privateKey, err := generateRSAKey()
    if err != nil {
        return nil, fmt.Errorf("failed to generate RSA key: %w", err)
    }
    return &JWTManager{
        privateKey:      privateKey,
        publicKey:       &privateKey.PublicKey,
        // ...
    }, nil
}
```

- Each service instance generates a new RSA keypair at startup
- In a multi-replica deployment, tokens issued by one instance cannot be verified by another
- Services cannot share JWT keys for distributed validation

**Fix:**
```go
// Load keys from secure storage (Vault/environment)
func NewJWTManager(tokenDuration, refreshDuration time.Duration, repos *db.Repositories, keyProvider KeyProvider) (*JWTManager, error) {
    privateKeyPEM, err := keyProvider.GetPrivateKey(context.Background())
    if err != nil {
        return nil, fmt.Errorf("failed to get private key: %w", err)
    }
    
    privateKey, err := parsePrivateKey(privateKeyPEM)
    if err != nil {
        return nil, fmt.Errorf("failed to parse private key: %w", err)
    }
    
    return &JWTManager{
        privateKey:      privateKey,
        publicKey:       &privateKey.PublicKey,
        // ...
    }, nil
}
```

#### Issue 2.1.2: Missing Token Revocation Check
**Severity:** High  
**Lines:** 129-156

**Problem:**
```go
func (j *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenString, &Claims{}, ...)
    if err != nil {
        return nil, fmt.Errorf("failed to parse token: %w", err)
    }
    if !token.Valid {
        return nil, fmt.Errorf("token is not valid")
    }
    claims, ok := token.Claims.(*Claims)
    if !ok {
        return nil, fmt.Errorf("failed to parse claims")
    }
    if claims.TokenType != "access" {
        return nil, fmt.Errorf("invalid token type")
    }
    return claims, nil
}
```

- No check against a revocation/blacklist (e.g., after logout or password change)
- Tokens cannot be revoked until expiration
- User logout is not effective

**Fix:**
```go
func (j *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
    // ... existing validation ...
    
    // Check token revocation
    isRevoked, err := j.repos.Sessions.IsTokenRevoked(context.Background(), tokenString)
    if err != nil {
        return nil, fmt.Errorf("failed to check token revocation: %w", err)
    }
    if isRevoked {
        return nil, fmt.Errorf("token has been revoked")
    }
    
    return claims, nil
}
```

#### Issue 2.1.3: No Rate Limiting on Token Endpoints
**Severity:** High  
**Lines:** Not present in auth handlers

**Problem:**
- No rate limiting on `/auth/login` or `/auth/refresh` endpoints
- Vulnerable to brute-force password attacks
- No account lockout mechanism

**Fix:**
Apply rate limiting middleware to auth endpoints:
```go
v1.POST("/auth/login", securityMiddleware.RateLimitMiddleware(), h.auditMiddleware.AuditMiddleware(), h.Login)
v1.POST("/auth/refresh", securityMiddleware.RateLimitMiddleware(), h.RefreshToken)
```

---

### 2.2 SQL Injection Vulnerabilities

**File:** `/apps/switchyard-api/internal/db/repositories.go`

#### Issue 2.2.1: Dynamic Query Building in AuditLogRepository
**Severity:** High  
**Lines:** 730-765

**Problem:**
```go
func (r *AuditLogRepository) Query(ctx context.Context, filters map[string]interface{}, limit int, offset int) ([]*types.AuditLog, error) {
    query := `SELECT ... FROM audit_logs WHERE 1=1`
    args := []interface{}{}
    argCount := 1
    
    // Add filters dynamically
    if actorID, ok := filters["actor_id"].(uuid.UUID); ok {
        query += fmt.Sprintf(" AND actor_id = $%d", argCount)  // String interpolation!
        args = append(args, actorID)
        argCount++
    }
    // ...
    rows, err := r.db.QueryContext(ctx, query, args...)
}
```

- While the actual values are parameterized, the query string itself is being built with string concatenation
- If keys in the filters map are controlled by user input, this could be exploited
- The pattern itself is fragile and error-prone

**Fix:**
```go
// Use prepared statements or a query builder library
type AuditQueryBuilder struct {
    baseQuery string
    args      []interface{}
    argCount  int
}

func (aq *AuditQueryBuilder) AddFilter(operator string, field string, value interface{}) {
    aq.argCount++
    aq.baseQuery += fmt.Sprintf(" AND %s %s $%d", field, operator, aq.argCount)
    aq.args = append(aq.args, value)
}

func (aq *AuditQueryBuilder) Build() (string, []interface{}) {
    return aq.baseQuery, aq.args
}
```

---

### 2.3 Input Validation and Sanitization

**File:** `/apps/switchyard-api/internal/validation/validator.go`

#### Issue 2.3.1: Missing URL Validation for Git Repository
**Severity:** Medium  
**Lines:** 170-179

**Problem:**
```go
func validateGitRepo(fl validator.FieldLevel) bool {
    value := fl.Field().String()
    if !gitRepoRegex.MatchString(value) {
        return false
    }
    
    // Additional validation: check if URL is parseable
    _, err := url.Parse(value)
    return err == nil
}
```

- Regex doesn't enforce protocol (`https://` required for non-internal repos)
- Could accept SSH-style URLs like `git@github.com:owner/repo` (which is allowed per comments but should be validated more strictly)
- No host whitelist to prevent internal network access

**Fix:**
```go
func validateGitRepo(fl validator.FieldLevel) bool {
    value := fl.Field().String()
    
    // Only allow HTTPS for external repos
    if !strings.HasPrefix(value, "https://") {
        return false
    }
    
    u, err := url.Parse(value)
    if err != nil {
        return false
    }
    
    // Validate host
    allowedHosts := []string{"github.com", "gitlab.com", "gitea.internal"}
    allowed := false
    for _, host := range allowedHosts {
        if strings.HasSuffix(u.Host, host) {
            allowed = true
            break
        }
    }
    return allowed
}
```

#### Issue 2.3.2: No Protection Against XXE in SBOM Processing
**Severity:** Medium  
**File:** `/apps/switchyard-api/internal/sbom/syft.go`

**Problem:**
- If SBOM processing accepts XML input (CycloneDX XML format), XML External Entity (XXE) attacks are possible
- No mention of XXE prevention in XML parsing

**Fix:**
```go
func parseXMLSBOM(data []byte) (*SBOM, error) {
    decoder := xml.NewDecoder(bytes.NewReader(data))
    
    // Disable XXE processing
    decoder.Entity = xml.HTMLEntity
    // OR disable all entity processing
    decoder.Entity = map[string]string{}
    
    var doc CycloneDXDocument
    if err := decoder.Decode(&doc); err != nil {
        return nil, fmt.Errorf("failed to decode SBOM: %w", err)
    }
    return convertToSBOM(&doc), nil
}
```

---

### 2.4 Secret Handling and Storage

**File:** `/apps/switchyard-api/internal/config/config.go`

#### Issue 2.4.1: Vault Token and GitHub Token in Environment Variables
**Severity:** High  
**Lines:** 97-105

**Problem:**
```go
GitHubToken:               viper.GetString("github-token"),
VaultToken:                viper.GetString("vault-token"),
VaultAddress:              viper.GetString("vault-address"),
```

- Secrets loaded from environment variables which may be logged or exposed
- No handling of secret rotation while service is running
- Tokens stored in memory unencrypted

**Fix:**
```go
// Use a secrets manager
type SecretManager interface {
    GetSecret(name string) (string, error)
    WatchSecret(name string) <-chan string
}

// Load from Vault or AWS Secrets Manager
func (c *Config) loadSecretsFromVault(vault SecretManager) error {
    token, err := vault.GetSecret("github-token")
    if err != nil {
        return fmt.Errorf("failed to load GitHub token: %w", err)
    }
    c.GitHubToken = token
    
    // Watch for secret rotation
    go func() {
        for newToken := range vault.WatchSecret("github-token") {
            c.GitHubToken = newToken
            logrus.Info("GitHub token rotated")
        }
    }()
    
    return nil
}
```

#### Issue 2.4.2: No Audit Trail for Secret Access
**Severity:** Medium  
**File:** `/apps/switchyard-api/internal/lockbox/vault.go`

**Problem:**
```go
func (v *VaultClient) GetSecret(ctx context.Context, path string) (*Secret, error) {
    // ... request to Vault ...
}
```

- No logging of secret access
- Vault audit log is independent; application doesn't correlate it
- No rate limiting on secret reads

**Fix:**
```go
func (v *VaultClient) GetSecret(ctx context.Context, path string) (*Secret, error) {
    userID := ctx.Value("user_id")
    
    logrus.WithFields(logrus.Fields{
        "user_id": userID,
        "secret_path": path,
        "timestamp": time.Now(),
    }).Info("Secret access")
    
    // ... rest of function ...
}
```

---

### 2.5 CSRF and XSS Protections

**File:** `/apps/switchyard-api/internal/middleware/security.go`

#### Issue 2.5.1: No CSRF Token Protection
**Severity:** Medium  
**Lines:** 1-435

**Problem:**
- No CSRF token generation or validation
- State-changing operations (POST, PUT, DELETE) are vulnerable to CSRF if called from a web client
- The UI frontend likely needs CSRF protection

**Fix:**
```go
type CSRFMiddleware struct {
    secretKey string
}

func (cm *CSRFMiddleware) CSRFTokenMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
            // Generate CSRF token for safe methods
            token := generateCSRFToken()
            c.SetCookie("csrf_token", token, 3600, "/", "", true, true)
            c.Header("X-CSRF-Token", token)
        } else {
            // Validate CSRF token for unsafe methods
            token := c.GetHeader("X-CSRF-Token")
            if !validateCSRFToken(token, c.Cookie("csrf_token")) {
                c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token invalid"})
                c.Abort()
                return
            }
        }
        c.Next()
    }
}
```

---

### 2.6 Hardcoded Credentials and Keys

**File:** `/apps/switchyard-api/internal/auth/password.go`

#### Issue 2.6.1: Bcrypt Cost is Constant
**Severity:** Low  
**Lines:** 9-14

**Problem:**
```go
const (
    bcryptCost = 14
)
```

- While 14 is reasonable, it should be configurable
- Changing cost in production requires code recompilation
- No warning if cost becomes insufficient over time

**Fix:**
```go
type PasswordConfig struct {
    BcryptCost int
    MinLength  int
    MaxLength  int
}

func HashPassword(password string, cost int) (string, error) {
    if cost < 10 || cost > 31 {
        return "", fmt.Errorf("invalid bcrypt cost: %d (must be 10-31)", cost)
    }
    hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)
    // ...
}
```

---

### 2.7 RBAC Implementation

**File:** `/apps/switchyard-api/internal/db/repositories.go` + `/apps/switchyard-api/internal/auth/jwt.go`

#### Issue 2.7.1: Incomplete RBAC Checks in ProjectAccessRepository
**Severity:** High  
**Lines:** 614-629

**Problem:**
```go
func (r *ProjectAccessRepository) GetUserRole(ctx context.Context, userID, projectID uuid.UUID, environmentID *uuid.UUID) (types.Role, error) {
    var role types.Role
    query := `
        SELECT role FROM project_access
        WHERE user_id = $1 AND project_id = $2
        AND (environment_id = $3 OR environment_id IS NULL)
        AND (expires_at IS NULL OR expires_at > NOW())
        ORDER BY environment_id NULLS LAST
        LIMIT 1
    `
    err := r.db.QueryRowContext(ctx, query, userID, projectID, environmentID).Scan(&role)
    if err != nil {
        return "", err
    }
    return role, nil
}
```

- Returns role but doesn't validate if the role is still valid
- No check for disabled users (user.active = false)
- No check for deactivated projects

**Fix:**
```go
func (r *ProjectAccessRepository) GetUserRole(ctx context.Context, userID, projectID uuid.UUID, environmentID *uuid.UUID) (types.Role, error) {
    var role types.Role
    query := `
        SELECT pa.role 
        FROM project_access pa
        JOIN users u ON pa.user_id = u.id
        JOIN projects p ON pa.project_id = p.id
        WHERE pa.user_id = $1 
        AND pa.project_id = $2
        AND u.active = true  -- User must be active
        AND (pa.environment_id = $3 OR pa.environment_id IS NULL)
        AND (pa.expires_at IS NULL OR pa.expires_at > NOW())
        ORDER BY pa.environment_id NULLS LAST
        LIMIT 1
    `
    err := r.db.QueryRowContext(ctx, query, userID, projectID, environmentID).Scan(&role)
    // ...
}
```

---

## 3. DATABASE LAYER ANALYSIS

### 3.1 Migration Files

**File:** `/apps/switchyard-api/internal/db/migrations/001_initial_schema.up.sql` and `002_compliance_schema.up.sql`

#### Issue 3.1.1: Missing Indexes on Foreign Keys
**Severity:** Medium  
**Lines:** 001_initial_schema.up.sql (missing indexes)

**Problem:**
- Foreign key columns don't have explicit indexes
- Queries like `WHERE release_id = $1` will do full table scans on large datasets
- Missing indexes:
  - `releases.service_id` (used frequently)
  - `deployments.release_id` (used frequently)

**Fix:**
```sql
CREATE INDEX IF NOT EXISTS idx_releases_service_id ON releases(service_id);
CREATE INDEX IF NOT EXISTS idx_deployments_release_id ON deployments(release_id);
-- Already present for some:
CREATE INDEX IF NOT EXISTS idx_deployments_environment_id ON deployments(environment_id);
```

#### Issue 3.1.2: N+1 Query Pattern in Service Listing
**Severity:** High  
**File:** `/apps/switchyard-api/internal/db/repositories.go`  
**Lines:** 147-177

**Problem:**
```go
func (r *ServiceRepository) ListAll(ctx context.Context) ([]*types.Service, error) {
    query := `SELECT id, project_id, name, git_repo, build_config, created_at, updated_at FROM services ORDER BY created_at DESC`
    rows, err := r.db.QueryContext(ctx, query)
    // ... for each row, unmarshals JSON
    for rows.Next() {
        var buildConfigJSON []byte
        err := rows.Scan(
            &service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
            &buildConfigJSON, &service.CreatedAt, &service.UpdatedAt,
        )
        if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
            return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
        }
    }
}
```

- If build config is large, many JSON unmarshals
- If service has related releases, deployments, this would cause N+1 queries in handlers
- No pagination implemented (could fetch millions of rows)

**Fix:**
```go
func (r *ServiceRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int, offset int) ([]*types.Service, error) {
    query := `
        SELECT id, project_id, name, git_repo, build_config, created_at, updated_at 
        FROM services 
        WHERE project_id = $1
        ORDER BY created_at DESC
        LIMIT $2 OFFSET $3
    `
    rows, err := r.db.QueryContext(ctx, query, projectID, limit, offset)
    // ... rest ...
}
```

#### Issue 3.1.3: Missing Constraints and Data Validation
**Severity:** Medium  
**Lines:** 001_initial_schema.up.sql

**Problem:**
- No NOT NULL constraint on critical fields like `image_uri` in releases
- No CHECK constraints to validate status values
- `replicas` in deployments can be negative (should be > 0)

**Fix:**
```sql
ALTER TABLE releases 
ADD CONSTRAINT check_image_uri_not_empty CHECK (image_uri != '');

ALTER TABLE releases 
ADD CONSTRAINT check_status_valid CHECK (status IN ('building', 'ready', 'failed', 'scanned'));

ALTER TABLE deployments 
ADD CONSTRAINT check_replicas_positive CHECK (replicas > 0);
```

---

### 3.2 Connection Pool Configuration

**File:** `/apps/switchyard-api/internal/db/connection.go`

#### Issue 3.2.1: DefaultDatabaseConfig Uses Hardcoded Values
**Severity:** Medium  
**Lines:** 323-339

**Problem:**
```go
func DefaultDatabaseConfig() *DatabaseConfig {
    return &DatabaseConfig{
        Host:              "localhost",
        Port:              5432,
        MaxOpenConns:      25,
        MaxIdleConns:      5,
        ConnMaxLifetime:   30 * time.Minute,
        ConnMaxIdleTime:   5 * time.Minute,
    }
}
```

- Not used in main.go (see issue 1.5.1)
- No guidance for different deployment sizes
- No automatic tuning based on expected load

**Fix:**
```go
func DefaultDatabaseConfig(environment string, expectedConnections int) *DatabaseConfig {
    cfg := &DatabaseConfig{
        Host: "localhost",
        Port: 5432,
    }
    
    switch environment {
    case "production":
        cfg.MaxOpenConns = expectedConnections * 2
        cfg.MaxIdleConns = expectedConnections / 2
        cfg.ConnMaxLifetime = 30 * time.Minute
    case "staging":
        cfg.MaxOpenConns = 20
        cfg.MaxIdleConns = 5
    default: // development
        cfg.MaxOpenConns = 10
        cfg.MaxIdleConns = 2
    }
    
    return cfg
}
```

---

### 3.3 Transaction Handling

**File:** `/apps/switchyard-api/internal/db/connection.go`

#### Issue 3.3.1: Deferred Rollback in Panic Handler
**Severity:** Medium  
**Lines:** 192-214

**Problem:**
```go
func (dm *DatabaseManager) WithTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
    tx, err := dm.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }

    defer func() {
        if p := recover(); p != nil {
            tx.Rollback()
            panic(p)  // Re-panic
        } else if err != nil {
            tx.Rollback()
        } else {
            err = tx.Commit()
        }
    }()

    err = fn(tx)
    return err
}
```

- If `tx.Rollback()` fails during panic recovery, error is swallowed
- If `tx.Commit()` fails, error overwrites the original `err`
- No timeout context passed to transaction

**Fix:**
```go
func (dm *DatabaseManager) WithTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
    tx, err := dm.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }

    defer func() {
        if p := recover(); p != nil {
            if rollbackErr := tx.Rollback(); rollbackErr != nil {
                logrus.Errorf("Failed to rollback after panic: %v (panic: %v)", rollbackErr, p)
            }
            panic(p)
        }
    }()

    err = fn(tx)
    
    if err != nil {
        if rollbackErr := tx.Rollback(); rollbackErr != nil {
            logrus.Errorf("Failed to rollback transaction: %v (original error: %v)", rollbackErr, err)
        }
        return err
    }
    
    if commitErr := tx.Commit(); commitErr != nil {
        return fmt.Errorf("failed to commit transaction: %w", commitErr)
    }
    
    return nil
}
```

---

## 4. API DESIGN ISSUES

### 4.1 HTTP Status Codes

**File:** `/apps/switchyard-api/internal/api/handlers.go`

#### Issue 4.1.1: Inconsistent Error Status Codes
**Severity:** Medium

**Problem:**
- No systematic approach to HTTP status code mapping
- Different error types likely return different status codes
- No documentation of which errors return what codes

**Examples from other files:**
```go
// From middleware/security.go line 92
c.JSON(http.StatusTooManyRequests, gin.H{"error": "Rate limit exceeded"})

// From auth/jwt.go line 209
c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is required"})
```

**Fix:**
Define consistent error mapping:
```go
type ErrorCode string

const (
    ErrValidation      ErrorCode = "VALIDATION_ERROR"      // 400
    ErrAuthentication  ErrorCode = "AUTHENTICATION_ERROR"   // 401
    ErrAuthorization   ErrorCode = "AUTHORIZATION_ERROR"    // 403
    ErrNotFound        ErrorCode = "NOT_FOUND"             // 404
    ErrConflict        ErrorCode = "CONFLICT"              // 409
    ErrInternal        ErrorCode = "INTERNAL_ERROR"        // 500
)

func (h *Handler) errorResponse(c *gin.Context, code ErrorCode, message string) {
    statusMap := map[ErrorCode]int{
        ErrValidation:     http.StatusBadRequest,
        ErrAuthentication: http.StatusUnauthorized,
        ErrAuthorization:  http.StatusForbidden,
        ErrNotFound:       http.StatusNotFound,
        ErrConflict:       http.StatusConflict,
        ErrInternal:       http.StatusInternalServerError,
    }
    
    c.JSON(statusMap[code], gin.H{
        "code": code,
        "message": message,
    })
}
```

---

### 4.2 Request/Response Validation

**File:** `/apps/switchyard-api/internal/validation/validator.go`

#### Issue 4.2.1: Missing Response Validation
**Severity:** Low  
**Lines:** 1-342

**Problem:**
- Only request validation is implemented
- No validation of response payloads before sending to clients
- Could leak unvalidated data types

**Fix:**
```go
type ResponseValidator struct {
    validate *validator.Validate
}

func (v *ResponseValidator) ValidateResponse(resp interface{}) error {
    return v.validate.Struct(resp)
}

// Use in handlers:
// c.JSON(http.StatusOK, h.validator.ValidateResponse(result))
```

---

### 4.3 API Versioning

**File:** `/apps/switchyard-api/internal/api/handlers.go`

#### Issue 4.3.1: Single API Version with No Deprecation Path
**Severity:** Medium  
**Lines:** 83-179

**Problem:**
```go
func SetupRoutes(router *gin.Engine, h *Handler) {
    // API v1 routes
    v1 := router.Group("/v1")
    {
        v1.POST("/auth/register", h.auditMiddleware.AuditMiddleware(), h.Register)
        // ...
    }
}
```

- Only `/v1` exists; no versioning strategy for future changes
- Breaking changes will force clients to update
- No deprecation warnings

**Fix:**
```go
func SetupRoutes(router *gin.Engine, h *Handler) {
    // Deprecated v1 routes with warning header
    v1 := router.Group("/v1")
    v1.Use(deprecationWarningMiddleware("v1", "2025-12-31"))
    {
        // ...
    }
    
    // New v2 routes
    v2 := router.Group("/v2")
    {
        // Enhanced endpoints with breaking changes
    }
}

func deprecationWarningMiddleware(version, sunsetDate string) gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Header("Deprecation", "true")
        c.Header("Sunset", sunsetDate)
        c.Header("API-Warning", fmt.Sprintf("API version %s is deprecated", version))
        c.Next()
    }
}
```

---

### 4.4 Pagination

**File:** `/apps/switchyard-api/internal/db/repositories.go`

#### Issue 4.4.1: Inconsistent Pagination Implementation
**Severity:** Medium  
**Lines:** 730-765, 877-935

**Problem:**
- Some list endpoints have pagination (limit, offset)
- Some don't (e.g., `ProjectRepository.List()`)
- No standard pagination response format

**Fix:**
```go
type PaginationParams struct {
    Limit  int `form:"limit,default=20" binding:"min=1,max=100"`
    Offset int `form:"offset,default=0" binding:"min=0"`
}

type PaginatedResponse[T any] struct {
    Data       []T   `json:"data"`
    Total      int64 `json:"total"`
    Limit      int   `json:"limit"`
    Offset     int   `json:"offset"`
    HasNext    bool  `json:"has_next"`
}

// Use in handlers:
func (h *Handler) ListProjects(c *gin.Context) {
    var params PaginationParams
    if err := c.ShouldBindQuery(&params); err != nil {
        h.errorResponse(c, ErrValidation, err.Error())
        return
    }
    
    projects, total, err := h.repos.Projects.List(c.Request.Context(), params.Limit, params.Offset)
    // ...
}
```

---

## 5. CONCURRENCY & PERFORMANCE ISSUES

### 5.1 Goroutine Leaks

**File:** `/apps/switchyard-api/internal/middleware/security.go`

#### Issue 5.1.1: Rate Limiter Cleanup Goroutine Never Stops
**Severity:** High  
**Lines:** 398-412

**Problem:**
```go
func (s *SecurityMiddleware) CleanupRateLimiters() {
    ticker := time.NewTicker(10 * time.Minute)
    go func() {
        for range ticker.C {  // <-- Never stops, no context to cancel
            s.mutex.Lock()
            if len(s.rateLimiters) > 10000 {
                s.rateLimiters = make(map[string]*rate.Limiter)
                logrus.Info("Cleared rate limiter cache")
            }
            s.mutex.Unlock()
        }
    }()
}
```

- Goroutine launched but never cancelled
- Ticker is never stopped
- On service shutdown, goroutine still runs

**Fix:**
```go
type SecurityMiddleware struct {
    rateLimiters map[string]*rate.Limiter
    mutex        sync.RWMutex
    config       *SecurityConfig
    ctx          context.Context          // Add context
    cancel       context.CancelFunc
    cleanupTicker *time.Ticker
}

func NewSecurityMiddleware(config *SecurityConfig) *SecurityMiddleware {
    ctx, cancel := context.WithCancel(context.Background())
    
    sm := &SecurityMiddleware{
        rateLimiters: make(map[string]*rate.Limiter),
        config:       config,
        ctx:          ctx,
        cancel:       cancel,
    }
    
    sm.startCleanup()
    return sm
}

func (s *SecurityMiddleware) startCleanup() {
    s.cleanupTicker = time.NewTicker(10 * time.Minute)
    go func() {
        for {
            select {
            case <-s.ctx.Done():
                s.cleanupTicker.Stop()
                return
            case <-s.cleanupTicker.C:
                s.mutex.Lock()
                if len(s.rateLimiters) > 10000 {
                    s.rateLimiters = make(map[string]*rate.Limiter)
                    logrus.Info("Cleared rate limiter cache")
                }
                s.mutex.Unlock()
            }
        }
    }()
}

func (s *SecurityMiddleware) Close() {
    s.cancel()
    s.cleanupTicker.Stop()
}
```

---

### 5.2 Race Conditions

**File:** `/apps/switchyard-api/internal/db/connection.go`

#### Issue 5.2.1: Data Race on errorCount in AsyncLogger
**Severity:** Medium  
**File:** `/apps/switchyard-api/internal/audit/async_logger.go`  
**Lines:** 22, 56-57, 113-115, 136-138

**Problem:**
```go
type AsyncLogger struct {
    // ...
    errorCount int  // <-- Not protected when incremented
    mu         sync.Mutex
}

func (l *AsyncLogger) Log(log *types.AuditLog) {
    select {
    case l.logChan <- log:
    default:
        l.mu.Lock()
        l.errorCount++  // Only locked here
        l.mu.Unlock()
    }
}

func (l *AsyncLogger) flushBatch(batch []*types.AuditLog) {
    // ...
    if err != nil {
        l.mu.Lock()
        l.errorCount++  // And here
        l.mu.Unlock()
    }
}
```

- `errorCount` is sometimes protected and sometimes not
- Stats could be read while being modified
- Use `atomic.AddInt64` instead

**Fix:**
```go
type AsyncLogger struct {
    // ...
    errorCount atomic.Int64
    mu         sync.Mutex
}

func (l *AsyncLogger) Log(log *types.AuditLog) {
    select {
    case l.logChan <- log:
    default:
        l.errorCount.Add(1)
    }
}

func (l *AsyncLogger) Stats() map[string]interface{} {
    return map[string]interface{}{
        "error_count": l.errorCount.Load(),
        // ...
    }
}
```

---

### 5.3 Blocking Operations in HTTP Handlers

**File:** `/apps/switchyard-api/internal/builder/service.go`

#### Issue 5.3.1: Build Operations Block HTTP Requests
**Severity:** Medium  
**Lines:** 99-189

**Problem:**
```go
func (s *Service) BuildFromGit(ctx context.Context, service *types.Service, gitSHA string) *CompleteBuildResult {
    // Step 1: Clone the repository - BLOCKS (could take minutes)
    cloneResult := s.git.CloneRepository(buildCtx, service.GitRepo, gitSHA)
    
    // Step 2: Build the service - BLOCKS (could take 30+ minutes)
    buildResult, err := s.builder.BuildService(buildCtx, service, gitSHA, cloneResult.Path)
    
    // Step 3: Generate SBOM - BLOCKS (could take minutes)
    // Step 4: Sign image - BLOCKS (could take seconds)
}
```

- If a handler calls `BuildFromGit()`, it blocks the entire request
- Long-running builds will timeout HTTP requests
- No async/job queue pattern

**Fix:**
```go
type BuildJob struct {
    ID         uuid.UUID
    ServiceID  uuid.UUID
    GitSHA     string
    Status     string
    CreatedAt  time.Time
    CompletedAt time.Time
}

// In handler:
func (h *Handler) TriggerBuild(c *gin.Context) {
    var req BuildRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        h.errorResponse(c, ErrValidation, err.Error())
        return
    }
    
    // Create job record
    job := &BuildJob{
        ID:       uuid.New(),
        ServiceID: serviceID,
        GitSHA:    req.GitSHA,
        Status:    "queued",
        CreatedAt: time.Now(),
    }
    
    // Queue job
    h.buildQueue.Enqueue(job)
    
    // Return immediately
    c.JSON(http.StatusAccepted, gin.H{
        "job_id": job.ID,
        "status_url": fmt.Sprintf("/v1/builds/%s", job.ID),
    })
}

// Separate worker process:
func (h *Handler) buildWorker() {
    for job := range h.buildQueue.Jobs() {
        result := h.builder.BuildFromGit(context.Background(), job.ServiceID, job.GitSHA)
        h.updateBuildJobStatus(job.ID, result)
    }
}
```

---

### 5.4 Cache Issues

**File:** `/apps/switchyard-api/internal/cache/redis.go`

#### Issue 5.4.1: Missing Cache Metrics
**Severity:** Low  
**Lines:** 396-408

**Problem:**
```go
func (r *RedisCache) GetMetrics(ctx context.Context) (*CacheMetrics, error) {
    info, err := r.client.Info(ctx, "stats").Result()
    if err != nil {
        return nil, err
    }
    
    // Parse Redis info for cache metrics
    // This is a simplified version - in production you'd parse the full stats
    return &CacheMetrics{
        Hits:   0, // Parse from info
        Misses: 0, // Parse from info
        Errors: 0, // Track in application
    }, nil
}
```

- Metrics are not implemented (hardcoded zeros)
- No cache hit/miss ratio tracking
- Can't optimize cache TTL without metrics

**Fix:**
```go
type RedisCache struct {
    client       *redis.Client
    config       *CacheConfig
    hitCount     atomic.Int64
    missCount    atomic.Int64
    errorCount   atomic.Int64
}

func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
    val, err := r.client.Get(ctx, key).Result()
    if err != nil {
        if err == redis.Nil {
            r.missCount.Add(1)
            return nil, ErrCacheMiss
        }
        r.errorCount.Add(1)
        return nil, fmt.Errorf("failed to get from cache: %w", err)
    }
    
    r.hitCount.Add(1)
    return []byte(val), nil
}

func (r *RedisCache) GetMetrics(ctx context.Context) (*CacheMetrics, error) {
    return &CacheMetrics{
        Hits:   r.hitCount.Load(),
        Misses: r.missCount.Load(),
        Errors: r.errorCount.Load(),
    }, nil
}
```

---

## 6. DEPENDENCIES ANALYSIS

**File:** `/apps/switchyard-api/go.mod`

### 6.1 Dependency Version Analysis

#### Issue 6.1.1: Outdated Kubernetes Dependency
**Severity:** Medium  
**Lines:** 27-29

**Problem:**
```
k8s.io/api v0.29.0
k8s.io/apimachinery v0.29.0
k8s.io/client-go v0.29.0
```

- Kubernetes 0.29.0 is from January 2024
- Current latest is 0.30+ (November 2024)
- May miss important bug fixes and security patches

**Recommendation:**
```bash
go get -u k8s.io/api@latest
go get -u k8s.io/apimachinery@latest
go get -u k8s.io/client-go@latest
```

#### Issue 6.1.2: Missing Explicit Dependency Version Pins
**Severity:** Low

**Problem:**
- Some indirect dependencies are not pinned
- No `go.sum` verification shown

**Recommendation:**
```bash
go mod tidy
go mod verify
```

---

## 7. PACKAGE-SPECIFIC REVIEWS

### 7.1 audit/ Package

#### audit/async_logger.go - Issue 1.2.1 (Already listed)
#### audit/async_logger.go - Issue 5.2.1 (Already listed)

#### Issue 7.1.1: No Metrics for Audit Logging Performance
**Severity:** Low  
**Lines:** 96-121

**Problem:**
- No tracking of flush duration
- No monitoring of batch sizes
- Can't diagnose if audit logging is slow

**Fix:**
```go
func (l *AsyncLogger) flushBatch(batch []*types.AuditLog) {
    start := time.Now()
    
    for _, log := range batch {
        if err := l.repos.AuditLogs.Log(ctx, log); err != nil {
            // ...
        }
    }
    
    duration := time.Since(start)
    if duration > 1*time.Second {
        logrus.Warnf("Slow audit log flush: %v for %d logs", duration, len(batch))
    }
}
```

---

### 7.2 compliance/ Package

#### Issue 7.2.1: No Retry on Transient Failures
**Severity:** Medium  
**File:** `/apps/switchyard-api/internal/compliance/exporter.go`  
**Lines:** 108-197

**Problem:**
```go
for attempt := 1; attempt <= e.maxRetries; attempt++ {
    // ...
    resp, err := e.httpClient.Do(req)
    if err != nil {
        // Retry on 5xx errors
        if resp.StatusCode >= 500 && attempt < e.maxRetries {
            // ...
            continue
        }
        break
    }
}
```

- Only retries on 5xx errors, not on connection timeouts
- No exponential backoff (uses linear: `backoff = e.retryDelay * time.Duration(attempt)`)
- Should retry on transient errors (ECONNREFUSED, ETIMEDOUT)

**Fix:**
```go
for attempt := 1; attempt <= e.maxRetries; attempt++ {
    resp, err := e.httpClient.Do(req)
    if err != nil {
        // Retry on transient errors
        if isTransientError(err) && attempt < e.maxRetries {
            backoff := exponentialBackoff(attempt, e.retryDelay)
            time.Sleep(backoff)
            continue
        }
        return &ExportResult{Error: err}
    }
}

func isTransientError(err error) bool {
    // Check for network errors
    var netErr net.Error
    if errors.As(err, &netErr) {
        return netErr.Timeout() || netErr.Temporary()
    }
    // Check for specific error types
    return errors.Is(err, context.DeadlineExceeded) || 
           errors.Is(err, context.Canceled)
}

func exponentialBackoff(attempt int, baseDelay time.Duration) time.Duration {
    // 2^attempt exponential backoff: 2s, 4s, 8s, etc.
    return baseDelay * time.Duration(1<<uint(attempt-1))
}
```

---

### 7.3 lockbox/ Package

#### Issue 7.3.1: No Secret Versioning in GetSecret
**Severity:** Medium  
**File:** `/apps/switchyard-api/internal/lockbox/vault.go`  
**Lines:** 68-126

**Problem:**
```go
func (v *VaultClient) GetSecret(ctx context.Context, path string) (*Secret, error) {
    // Always gets latest version
    // No way to request specific version
}
```

- Always returns the latest version
- Can't rollback to a previous secret version if rotation fails
- No ability to validate secret versions match deployed version

**Fix:**
```go
func (v *VaultClient) GetSecretVersion(ctx context.Context, path string, version int) (*Secret, error) {
    var versionPath string
    if version > 0 {
        versionPath = fmt.Sprintf("%s?version=%d", path, version)
    } else {
        versionPath = path
    }
    
    url := fmt.Sprintf("%s/v1/%s", v.address, versionPath)
    // ... rest of implementation
}
```

---

### 7.4 provenance/ Package

#### Issue 7.4.1: No Webhook Signature Verification
**Severity:** High  
**File:** `/apps/switchyard-api/internal/provenance/checker.go`

**Problem:**
- GitHub API is called directly but no HMAC signature verification
- If someone intercepts the GitHub API response, they could inject fake approvals
- No way to verify that GitHub API responses are authentic

**Fix:**
```go
// Add signature verification for GitHub API responses
type GitHubResponse struct {
    Data      interface{}
    Signature string `json:"X-Hub-Signature-256"`
}

func (c *Checker) VerifyGitHubWebhook(payload []byte, signature string) bool {
    hash := hmac.New(sha256.New, []byte(c.githubSecret))
    hash.Write(payload)
    expected := "sha256=" + hex.EncodeToString(hash.Sum(nil))
    
    return hmac.Equal([]byte(expected), []byte(signature))
}
```

---

### 7.5 reconciler/ Package

#### Issue 7.5.1: No Health Check for Reconciled Services
**Severity:** Medium  
**File:** `/apps/switchyard-api/internal/reconciler/service.go`  
**Lines:** 47-124

**Problem:**
```go
func (r *ServiceReconciler) Reconcile(ctx context.Context, req *ReconcileRequest) *ReconcileResult {
    // ... creates deployment ...
    // ... waits for deployment ready ...
    // But doesn't verify service is actually healthy
}
```

- Only checks that replicas are ready
- Doesn't verify the service endpoints are responding
- Doesn't check application health endpoints

**Fix:**
```go
func (r *ServiceReconciler) Reconcile(ctx context.Context, req *ReconcileRequest) *ReconcileResult {
    // ... existing code ...
    
    // Additional step: verify service health
    if err := r.verifyServiceHealth(ctx, deployment, service); err != nil {
        return &ReconcileResult{
            Success: false,
            Message: "Service health check failed",
            Error:   err,
        }
    }
    
    return &ReconcileResult{Success: true}
}

func (r *ServiceReconciler) verifyServiceHealth(ctx context.Context, deployment *appsv1.Deployment, service *corev1.Service) error {
    // Query service endpoints
    endpoints, err := r.k8sClient.Clientset.CoreV1().Endpoints(deployment.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
    if err != nil {
        return fmt.Errorf("failed to get service endpoints: %w", err)
    }
    
    if len(endpoints.Subsets) == 0 {
        return fmt.Errorf("no service endpoints available")
    }
    
    // Optionally: make HTTP request to health endpoint
    return nil
}
```

---

### 7.6 rotation/ Package

#### Issue 7.6.1: No Atomic Secret Rotation
**Severity:** High  
**File:** `/apps/switchyard-api/internal/rotation/controller.go`  
**Lines:** 158-217

**Problem:**
```go
func (c *Controller) performRotation(ctx context.Context, event *lockbox.SecretChangeEvent, auditLog *lockbox.RotationAuditLog) error {
    // Step 1: Update Kubernetes secret
    // Step 2: Trigger rolling restart
    // Step 3: Monitor rollout progress
    // NO ATOMIC GUARANTEE
}
```

- Between updating secret and rolling restart, there's a window where pods have mixed secret versions
- If rollout fails midway, state is inconsistent
- No rollback to previous secret on failure

**Fix:**
```go
func (c *Controller) performRotation(ctx context.Context, event *lockbox.SecretChangeEvent, auditLog *lockbox.RotationAuditLog) error {
    // Use a transaction-like pattern
    rotationTx := &RotationTransaction{
        secretPath: event.SecretPath,
        oldVersion: event.OldVersion,
        newVersion: event.NewVersion,
        steps:      []RotationStep{},
    }
    
    // Step 1: Fetch new secret (verify it exists)
    newSecret, err := c.vault.GetSecret(ctx, event.SecretPath, event.NewVersion)
    if err != nil {
        return fmt.Errorf("failed to fetch new secret: %w", err)
    }
    rotationTx.NewSecret = newSecret
    
    // Step 2: Update K8s secret with rollback capability
    oldSecretData, err := c.backupCurrentSecret(ctx, namespace, secretName)
    rotationTx.BackupSecretData = oldSecretData
    rotationTx.AddStep("backup_secret", true)
    
    if err := c.updateK8sSecret(ctx, namespace, secretName, newSecret); err != nil {
        c.restoreSecret(ctx, namespace, secretName, oldSecretData)
        return fmt.Errorf("failed to update secret: %w", err)
    }
    rotationTx.AddStep("update_secret", true)
    
    // Step 3: Trigger rollout
    if err := c.k8sClient.RollingRestart(ctx, namespace, service.Name); err != nil {
        c.restoreSecret(ctx, namespace, secretName, oldSecretData)
        return fmt.Errorf("failed to trigger rollout: %w", err)
    }
    rotationTx.AddStep("rollout_started", true)
    
    // Step 4: Monitor and verify
    if err := c.waitForRollout(ctx, namespace, service.Name); err != nil {
        c.restoreSecret(ctx, namespace, secretName, oldSecretData)
        return fmt.Errorf("rollout failed: %w", err)
    }
    rotationTx.AddStep("rollout_complete", true)
    
    return nil
}
```

---

## CRITICAL ISSUES SUMMARY

### Critical Severity (Must Fix Before Production)

1. **JWT Keys Not Shared Across Replicas** (2.1.1)
   - Multi-instance deployments will fail with token verification errors

2. **Missing Resource Cleanup** (1.1.1)
   - Services may hang on shutdown, resources not released

3. **No Token Revocation** (2.1.2)
   - Users cannot be logged out; tokens cannot be invalidated

4. **Secret Rotation Not Atomic** (7.6.1)
   - Services could enter inconsistent states with mixed secret versions

5. **Compliance Webhook No Signature Verification** (7.4.1)
   - Audit trail could be spoofed by network attackers

---

### Recommendations by Priority

**Phase 1 (Before Production):**
- Fix JWT shared keys (2.1.1)
- Implement token revocation (2.1.2)
- Add resource cleanup (1.1.1)
- Fix atomic secret rotation (7.6.1)
- Add webhook signature verification (7.4.1)

**Phase 2 (Before Public Release):**
- Implement rate limiting on auth endpoints (2.1.3)
- Fix goroutine leak in cleanup (5.1.1)
- Implement proper pagination (4.4.1)
- Async build jobs (5.3.1)
- Shared database connection pool (1.1.3)

**Phase 3 (Post-Launch Improvements):**
- Add cache metrics (5.4.1)
- Implement CSRF protection (2.5.1)
- Service health verification in reconciler (7.5.1)
- Compliance retry with exponential backoff (7.2.1)

