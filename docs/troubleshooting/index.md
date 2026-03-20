---
title: Troubleshooting
description: Common issues and solutions for Enclii platform
sidebar_position: 1
tags: [troubleshooting, help, support]
---

# Troubleshooting Guide

This section contains solutions for common issues you may encounter when using the Enclii platform.

## Quick Diagnosis

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| API returns 401 | Invalid or expired token | [Auth Problems](./auth-problems) |
| API returns 500 | Server error | [API Errors](./api-errors) |
| Build stuck or failed | Configuration issue | [Build Failures](./build-failures) |
| Deploy timeout | Resource or network issue | [Deployment Issues](./deployment-issues) |
| SSL/DNS errors | Certificate or routing | [Networking](./networking) |

## Troubleshooting by Topic

### [API Errors](./api-errors)
Common API error codes, their meanings, and how to resolve them. Covers authentication errors, validation failures, and server-side issues.

### [Build Failures](./build-failures)
Diagnose and fix build pipeline issues including Buildpack/Dockerfile detection failures, dependency problems, and registry push errors.

### [Deployment Issues](./deployment-issues)
Resolve deployment problems including pod crashes, health check failures, resource limits, and rollback procedures.

### [Authentication Problems](./auth-problems)
Fix login issues, token problems, SSO configuration errors, and session management.

### [Networking Issues](./networking)
Troubleshoot DNS resolution, SSL certificates, Cloudflare tunnel problems, and routing errors.

## Getting Help

If you can't find a solution here:

1. **Check the logs**: Use `enclii logs <service> -f` for real-time logs
2. **Review recent changes**: Check `git log` and deployment history
3. **Search existing issues**: [GitHub Issues](https://github.com/madfam-org/enclii/issues)
4. **Open a new issue**: Include error messages, steps to reproduce, and relevant logs

## Related Documentation

- **Getting Started**: [Quickstart Guide](/getting-started/QUICKSTART)
- **CLI Reference**: [CLI Commands](/cli/)
- **FAQ**: [Frequently Asked Questions](/faq/)
- **Runbooks**: [Database Recovery](/runbooks/DATABASE_RECOVERY) | [Cluster Ops](/runbooks/CLUSTER_REMEDIATION_OPS)
