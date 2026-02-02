# Audit Logging Test Guide

## Overview

This guide provides step-by-step instructions for testing the audit logging middleware implementation in Switchyard API.

## What Was Implemented

### Components Created

1. **Audit Middleware** (`internal/audit/middleware.go`)
   - Automatic capture of all API mutations (POST, PUT, PATCH, DELETE)
   - Extracts user context from JWT tokens
   - Captures and sanitizes request bodies
   - Records resource type, ID, and action
   - Logs outcome (success/failure/denied)
   - Non-blocking async logging

2. **Async Logger** (`internal/audit/async_logger.go`)
   - Buffered channel for non-blocking log writes (minimum 1000 entries)
   - Fallback channel (50% of primary) for overflow
   - File-based JSONL fallback (`/var/log/enclii/audit-fallback.jsonl`) when both memory buffers overflow
   - Recovery worker (30-second tick) replays file fallback entries to DB and truncates on success
   - Batch processing (10 logs per batch)
   - Periodic flushing (every 5 seconds)
   - Graceful shutdown with pending log flush
   - Prometheus metrics: `enclii_audit_logs_file_fallback_total`, `enclii_audit_logs_dropped_total`
   - Error tracking and statistics

### Integration Points

- **Auth Routes**: Login and register are audited (even without authentication)
- **Protected Routes**: All mutations automatically audited after authentication
- **Sensitive Field Redaction**: Passwords, tokens, keys automatically redacted

## Prerequisites

1. **Database Running**: PostgreSQL with migrations applied
   ```bash
   # Check if database is accessible
   psql $ENCLII_DB_URL -c "SELECT 1"
   ```

2. **Migrations Applied**: Ensure `002_compliance_schema.up.sql` is applied
   ```bash
   # Check if audit_logs table exists
   psql $ENCLII_DB_URL -c "\d audit_logs"
   ```

3. **API Server Ready**: Build completed successfully
   ```bash
   cd /home/user/enclii
   go build -o bin/switchyard-api ./apps/switchyard-api/cmd/api/main.go
   ```

## Test Scenarios

### Test 1: User Registration (Unauthenticated Audit)

**Purpose**: Verify audit logging works for public endpoints

```bash
curl -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "audit-test@example.com",
    "password": "SecurePassword123!",
    "name": "Audit Test User"
  }'
```

**Expected Audit Log**:
- `action`: "user_register"
- `resource_type`: "user"
- `outcome`: "success"
- `actor_id`: (newly created user ID)
- `context.request_body.password`: "[REDACTED]"

**Verification Query**:
```sql
SELECT
  timestamp,
  actor_email,
  action,
  resource_type,
  outcome,
  context->'request_body'->>'password' as password_in_log
FROM audit_logs
WHERE action = 'user_register'
ORDER BY timestamp DESC
LIMIT 1;
```

**Expected Result**: `password_in_log` should be `[REDACTED]`

---

### Test 2: User Login (Failed Attempt)

**Purpose**: Verify failed login attempts are audited

```bash
curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "audit-test@example.com",
    "password": "WrongPassword123!"
  }'
```

**Expected Audit Log**:
- `action`: "login_failed"
- `outcome`: "failure"
- `context.reason`: "invalid_password"

**Verification Query**:
```sql
SELECT
  timestamp,
  actor_email,
  action,
  outcome,
  context->>'reason' as failure_reason
FROM audit_logs
WHERE action = 'login_failed'
  AND actor_email = 'audit-test@example.com'
ORDER BY timestamp DESC
LIMIT 1;
```

---

### Test 3: User Login (Successful)

**Purpose**: Verify successful login is audited

```bash
curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "audit-test@example.com",
    "password": "SecurePassword123!"
  }'
```

**Expected Audit Log**:
- `action`: "login_success"
- `outcome`: "success"
- `context.method`: "password"

**Save the access token** from response for next tests:
```bash
export TOKEN="<access_token_from_response>"
```

**Verification Query**:
```sql
SELECT
  timestamp,
  actor_email,
  action,
  outcome,
  context->>'method' as auth_method,
  ip_address,
  user_agent
FROM audit_logs
WHERE action = 'login_success'
  AND actor_email = 'audit-test@example.com'
ORDER BY timestamp DESC
LIMIT 1;
```

---

### Test 4: Create Project (Authenticated Mutation)

**Purpose**: Verify authenticated mutations are automatically audited

```bash
curl -X POST http://localhost:8080/v1/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Audit Test Project",
    "slug": "audit-test",
    "description": "Project for testing audit logging"
  }'
```

**Expected Audit Log**:
- `action`: "create_project"
- `resource_type`: "project"
- `resource_id`: (project slug or ID)
- `outcome`: "success"
- `context.status_code`: 201
- `context.duration_ms`: (request duration)

**Verification Query**:
```sql
SELECT
  timestamp,
  actor_email,
  action,
  resource_type,
  resource_id,
  outcome,
  context->>'status_code' as status_code,
  context->>'duration_ms' as duration_ms,
  context->'request_body'->>'name' as project_name
FROM audit_logs
WHERE action = 'create_project'
  AND actor_email = 'audit-test@example.com'
ORDER BY timestamp DESC
LIMIT 1;
```

---

### Test 5: Unauthorized Access (Permission Denied)

**Purpose**: Verify denied access attempts are audited

```bash
# Try to access endpoint without token
curl -X POST http://localhost:8080/v1/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Unauthorized Project",
    "slug": "unauthorized"
  }'
```

**Expected Result**: 401 Unauthorized, **no audit log** (middleware skips unauthenticated requests)

---

### Test 6: Async Logger Performance

**Purpose**: Verify async logging doesn't block requests

```bash
# Send 50 rapid-fire requests
for i in {1..50}; do
  curl -s -X POST http://localhost:8080/v1/projects \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{
      \"name\": \"Load Test Project $i\",
      \"slug\": \"load-test-$i\",
      \"description\": \"Performance test\"
    }" &
done
wait

# Wait for async logger to flush (max 5 seconds)
sleep 6
```

**Verification Query**:
```sql
SELECT
  COUNT(*) as total_logs,
  COUNT(DISTINCT actor_email) as unique_users,
  AVG((context->>'duration_ms')::int) as avg_duration_ms,
  MAX((context->>'duration_ms')::int) as max_duration_ms
FROM audit_logs
WHERE action = 'create_project'
  AND timestamp > NOW() - INTERVAL '1 minute';
```

**Expected Result**:
- `total_logs`: 50 (or close to it, accounting for failures)
- `avg_duration_ms`: < 500ms (requests should be fast)

---

### Test 7: Sensitive Field Redaction

**Purpose**: Verify sensitive fields are redacted in audit logs

```bash
# Create service with API key (sensitive)
curl -X POST http://localhost:8080/v1/projects/audit-test/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Secret Service",
    "git_repo": "https://github.com/example/secret.git",
    "build_config": {
      "env": {
        "API_KEY": "super-secret-key-12345",
        "DATABASE_PASSWORD": "very-secret-password",
        "PUBLIC_URL": "https://example.com"
      }
    }
  }'
```

**Verification Query**:
```sql
SELECT
  action,
  resource_type,
  context->'request_body'->'build_config'->'env'->>'API_KEY' as api_key,
  context->'request_body'->'build_config'->'env'->>'DATABASE_PASSWORD' as db_password,
  context->'request_body'->'build_config'->'env'->>'PUBLIC_URL' as public_url
FROM audit_logs
WHERE action = 'create_service'
  AND resource_name = 'Secret Service'
ORDER BY timestamp DESC
LIMIT 1;
```

**Expected Result**:
- `api_key`: "[REDACTED]"
- `db_password`: "[REDACTED]"
- `public_url`: "https://example.com" (not redacted, not sensitive)

---

### Test 8: Logout Audit

**Purpose**: Verify logout is audited

```bash
curl -X POST http://localhost:8080/v1/auth/logout \
  -H "Authorization: Bearer $TOKEN"
```

**Expected Audit Log**:
- `action`: "logout"
- `outcome`: "success"

**Verification Query**:
```sql
SELECT
  timestamp,
  actor_email,
  action,
  outcome
FROM audit_logs
WHERE action = 'logout'
  AND actor_email = 'audit-test@example.com'
ORDER BY timestamp DESC
LIMIT 1;
```

---

## Comprehensive Verification

### Check All Audit Logs for Test Session

```sql
SELECT
  timestamp,
  action,
  resource_type,
  outcome,
  context->>'status_code' as status_code,
  context->>'duration_ms' as duration_ms
FROM audit_logs
WHERE actor_email = 'audit-test@example.com'
ORDER BY timestamp ASC;
```

**Expected Actions in Order**:
1. `user_register` - success
2. `login_failed` - failure (wrong password)
3. `login_success` - success
4. `create_project` - success (1 or more)
5. `create_service` - success
6. `logout` - success

---

## AsyncLogger Statistics

### Check Logger Health

If the API exposes stats (future enhancement), you can check:

```go
// In a future admin endpoint
stats := h.auditMiddleware.GetStats()
// Returns:
// {
//   "buffer_size": 100,
//   "buffer_pending": 3,
//   "error_count": 0,
//   "batch_size": 10,
//   "flush_interval": "5s"
// }
```

---

## Performance Metrics

### Expected Performance Characteristics

| Metric | Target | How to Verify |
|--------|--------|---------------|
| Request overhead | < 5ms | Check `duration_ms` in context |
| Buffer capacity | 100 logs | Check buffer_pending in stats |
| Flush interval | 5 seconds | Logs appear within 5s |
| Batch size | 10 logs | Check database write patterns |
| Dropped logs | 0 | Check error_count in stats |
| File fallback writes | 0 (normal) | `enclii_audit_logs_file_fallback_total` Prometheus counter |
| Dropped logs (all fallbacks exhausted) | 0 | `enclii_audit_logs_dropped_total` Prometheus counter |

---

## Troubleshooting

### Logs Not Appearing

**Symptom**: No audit logs in database after requests

**Possible Causes**:
1. **Migrations not applied**: Run `002_compliance_schema.up.sql`
2. **Async logger buffer full**: Check error_count
3. **Database connection issue**: Check application logs
4. **Middleware not applied**: Verify route setup in handlers.go

**Debug Steps**:
```bash
# Check if audit_logs table exists
psql $ENCLII_DB_URL -c "SELECT COUNT(*) FROM audit_logs"

# Check application logs for errors
tail -f /var/log/switchyard-api.log | grep audit

# Check file-based fallback for buffered entries
cat /var/log/enclii/audit-fallback.jsonl

# Check if middleware is in route chain
curl -v http://localhost:8080/v1/projects
```

---

### Sensitive Data Leaked

**Symptom**: Passwords or keys visible in audit logs

**Check**:
```sql
SELECT
  context->'request_body'
FROM audit_logs
WHERE context->'request_body' IS NOT NULL
LIMIT 5;
```

**Fix**: Verify `isSensitiveField()` function in middleware.go includes all sensitive field names.

---

### Performance Degradation

**Symptom**: Requests taking longer than expected

**Check**:
```sql
SELECT
  action,
  AVG((context->>'duration_ms')::int) as avg_ms,
  MAX((context->>'duration_ms')::int) as max_ms
FROM audit_logs
GROUP BY action
ORDER BY avg_ms DESC;
```

**Possible Causes**:
1. **Async logger blocking**: Buffer full, requests waiting
2. **Database slow**: Check database performance
3. **Large request bodies**: Sanitization taking time

**Fix**: Increase buffer size or batch size in NewAsyncLogger().

---

## SOC 2 Compliance Checklist

Use audit logs to verify SOC 2 requirements:

### CC6.1 - Logical Access Controls

```sql
-- Verify all login attempts are logged
SELECT
  DATE(timestamp) as day,
  COUNT(*) FILTER (WHERE outcome = 'success') as successful_logins,
  COUNT(*) FILTER (WHERE outcome = 'failure') as failed_logins
FROM audit_logs
WHERE action IN ('login_success', 'login_failed')
GROUP BY DATE(timestamp)
ORDER BY day DESC;
```

### CC7.2 - System Operations Monitoring

```sql
-- Verify all mutations are logged
SELECT
  action,
  COUNT(*) as total,
  COUNT(*) FILTER (WHERE outcome = 'success') as successes,
  COUNT(*) FILTER (WHERE outcome = 'failure') as failures,
  COUNT(*) FILTER (WHERE outcome = 'denied') as denied
FROM audit_logs
WHERE action NOT LIKE '%login%'
GROUP BY action
ORDER BY total DESC;
```

### CC8.1 - Risk of Fraud Detection

```sql
-- Detect suspicious patterns (multiple failed logins)
SELECT
  actor_email,
  COUNT(*) as failed_attempts,
  MIN(timestamp) as first_attempt,
  MAX(timestamp) as last_attempt,
  ARRAY_AGG(DISTINCT ip_address) as source_ips
FROM audit_logs
WHERE action = 'login_failed'
  AND timestamp > NOW() - INTERVAL '24 hours'
GROUP BY actor_email
HAVING COUNT(*) >= 5
ORDER BY failed_attempts DESC;
```

---

## Success Criteria

The audit logging implementation is successful if:

- ✅ All mutations (POST, PUT, PATCH, DELETE) are logged
- ✅ Sensitive fields are redacted in audit logs
- ✅ Failed login attempts are logged
- ✅ Request performance overhead < 5ms
- ✅ No logs dropped (error_count = 0)
- ✅ Logs appear in database within 5 seconds
- ✅ Actor attribution (user ID, email, role) is captured
- ✅ Resource context (type, ID, name) is captured
- ✅ Outcome (success/failure/denied) is determined correctly
- ✅ Immutability enforced (cannot UPDATE or DELETE audit_logs)
- ✅ File-based JSONL fallback persists entries when DB is unavailable
- ✅ Recovery worker replays fallback entries to DB within 30 seconds

---

## Next Steps

After successful testing:

1. ~~**Monitor Production**: Set up alerts for high error_count~~ ✅ Done — Prometheus counters `enclii_audit_logs_file_fallback_total` and `enclii_audit_logs_dropped_total`
2. **Tune Performance**: Adjust buffer size and batch size based on load
3. **Add Metrics**: Expose AsyncLogger stats via admin endpoint
4. ~~**Archive Old Logs**: Implement log retention policy (e.g., 90 days)~~ ✅ Done — `004_audit_archive_support` migration + archive CronJob
5. **Compliance Reports**: Create dashboards for SOC 2 auditors

---

## Related Documentation

- [Sprint 0 Complete](./SPRINT_0_COMPLETE.md) - Authentication foundation
- [Compliance Gap Analysis](./switchyard_compliance_gap_analysis.md) - SOC 2 requirements
- Database Schema: `apps/switchyard-api/internal/db/migrations/002_compliance_schema.up.sql`
- Middleware Implementation: `apps/switchyard-api/internal/audit/middleware.go`
- Async Logger: `apps/switchyard-api/internal/audit/async_logger.go`
