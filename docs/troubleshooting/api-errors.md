---
title: API Errors
description: Common API error codes and their solutions
sidebar_position: 2
tags: [troubleshooting, api, errors]
---

# API Error Reference

This guide covers common API errors you may encounter when using the Enclii API.

## Prerequisites

- [CLI installed](/docs/cli/) or API client configured
- [Authentication set up](/docs/guides/cli-auth-setup)

## HTTP Status Codes

### 400 Bad Request

**Meaning**: The request body or parameters are malformed or invalid.

**Common Causes**:
- Missing required fields
- Invalid JSON format
- Field validation errors

**Solutions**:

```bash
# Check request format
curl -X POST https://api.enclii.dev/v1/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-service", "project_id": "uuid-here"}'
```

Verify:
- All required fields are present
- UUIDs are valid format
- Strings don't exceed max length

### 401 Unauthorized

**Meaning**: Authentication failed or token is missing/invalid.

**Common Causes**:
- Missing `Authorization` header
- Expired access token
- Invalid token format

**Solutions**:

```bash
# Re-authenticate
enclii login

# Verify token is valid
enclii whoami

# For API calls, ensure header format
curl -H "Authorization: Bearer $TOKEN" https://api.enclii.dev/v1/users/me
```

See [Authentication Problems](./auth-problems) for detailed auth troubleshooting.

### 403 Forbidden

**Meaning**: You don't have permission to access this resource.

**Common Causes**:
- Insufficient role permissions
- Resource belongs to another organization
- API key doesn't have required scope

**Solutions**:

```bash
# Check your current user/role
enclii whoami

# Verify project access
enclii projects list
```

Contact your organization admin if you need elevated permissions.

### 404 Not Found

**Meaning**: The requested resource doesn't exist.

**Common Causes**:
- Incorrect resource ID
- Resource was deleted
- Wrong API endpoint

**Solutions**:

```bash
# Verify resource exists
enclii services list --project <project-id>

# Check the correct ID
enclii services get <service-id>
```

### 409 Conflict

**Meaning**: The request conflicts with existing state.

**Common Causes**:
- Duplicate name within project
- Resource already exists
- Concurrent modification

**Solutions**:

```bash
# Check for existing resource
enclii services list | grep "my-service"

# Use a unique name
enclii services create --name "my-service-v2"
```

### 422 Unprocessable Entity

**Meaning**: Request syntax is valid but semantically incorrect.

**Common Causes**:
- Business rule violations
- Invalid state transitions
- Dependency errors

**Solutions**:
- Check the error message for specific field issues
- Verify the operation is valid for the resource's current state
- Ensure referenced resources exist (e.g., project before service)

### 429 Too Many Requests

**Meaning**: Rate limit exceeded.

**Common Causes**:
- Too many API calls in short period
- Aggressive polling

**Solutions**:

```bash
# Wait and retry
sleep 60

# Use exponential backoff in scripts
for i in {1..5}; do
  response=$(curl -s -w "%{http_code}" ...)
  if [ "$response" != "429" ]; then break; fi
  sleep $((2 ** i))
done
```

Rate limits:
- API: 100 requests/minute per user
- Webhooks: 1000 requests/minute per project

### 500 Internal Server Error

**Meaning**: Server-side error occurred.

**Common Causes**:
- Database connectivity issue
- Unhandled exception
- Infrastructure problem

**Solutions**:

1. **Retry the request** - transient errors may resolve
2. **Check status page** - https://status.enclii.dev
3. **Review API logs**:

```bash
# Prefer enclii CLI
enclii logs switchyard-api -n 50

# Or via kubectl if you need raw pod logs
kubectl logs -n enclii deploy/switchyard-api --tail=50
```

4. **Report the issue** with:
   - Timestamp
   - Request details (endpoint, method)
   - Response body
   - Request ID (from `X-Request-ID` header)

### 502/503/504 Gateway Errors

**Meaning**: Infrastructure or upstream service issue.

**Common Causes**:
- Service temporarily unavailable
- Cloudflare tunnel issue
- Pod restarting

**Solutions**:

1. **Wait and retry** - often resolves within 30 seconds
2. **Check service health**:

```bash
curl https://api.enclii.dev/health
```

3. **Verify infrastructure**:

```bash
# Service status via CLI
enclii ps

# Direct pod debugging (kubectl — when you need K8s event details)
kubectl describe pod -n enclii <pod-name>
```

## API-Specific Errors

### Build Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `build_failed` | Build process failed | Check [Build Failures](./build-failures) |
| `no_dockerfile` | Neither Dockerfile nor buildable project found | Add Dockerfile or ensure buildpack support |
| `registry_push_failed` | Can't push to container registry | Check registry credentials |

### Deployment Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `deployment_timeout` | Pods didn't become ready | Check [Deployment Issues](./deployment-issues) |
| `health_check_failed` | Health endpoint not responding | Verify health endpoint returns 200 |
| `resource_quota_exceeded` | Out of allocated resources | Contact admin to increase quota |

## Debugging Tips

### Enable Verbose Logging

```bash
# CLI verbose mode
enclii --verbose deploy

# Check response headers
curl -v https://api.enclii.dev/v1/users/me
```

### Check Request ID

Every API response includes `X-Request-ID` header. Include this when reporting issues:

```bash
curl -i https://api.enclii.dev/v1/users/me 2>&1 | grep -i x-request-id
```

## Related Documentation

- **Authentication**: [Auth Problems](./auth-problems)
- **API Reference**: [OpenAPI Spec](/api-reference)
- **CLI Reference**: [CLI Commands](/docs/cli/)
