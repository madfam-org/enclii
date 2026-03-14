# Disaster Recovery Runbook

> **Full runbook relocated to private repo for security (2026-03-13).**

Disaster recovery procedures, including SSH access commands, server IPs, node join procedures, and credential management, are maintained in:

**`madfam-org/internal-devops`** → `runbooks/disaster-recovery.md`

Contact @aldoruizluna for access.

## Quick Reference (Non-Sensitive)

| Scenario | Severity | Estimated RTO |
|----------|----------|---------------|
| Single pod crash | Low | Automatic (<2 min) |
| PostgreSQL failure | High | 30 min |
| Redis failure | Medium | 5 min |
| Longhorn volume failure | High | 30-60 min |
| Worker node failure | Medium | 15 min |
| Control plane failure | Critical | 2-4 hours |
| Cloudflare Tunnel down | High | 5-15 min |
| Full cluster rebuild | Critical | 2-4 hours |

## RPO / RTO

- **RPO**: 24 hours (daily PostgreSQL backups to R2)
- **RTO**: 2-4 hours (full cluster rebuild + restore)

## Related Documentation

- [Database Recovery](./DATABASE_RECOVERY.md)
- [Infrastructure Anatomy](../infrastructure/INFRA_ANATOMY.md)
- [GitOps with ArgoCD](../infrastructure/GITOPS.md)
- [Storage (Longhorn)](../infrastructure/STORAGE.md)
