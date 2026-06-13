# Runbook: Postgres HA Cutover (single-instance → CNPG `postgres-ha`)

**Last Updated:** 2026-06-13
**Owner:** Platform / DB on-call + operator approver (RFC 0012 §8 gate)
**Blocker:** First-Pesos roadmap blocker #1 — the shared `data/postgres`
single-instance Deployment backs every ecosystem DB, including the Dhanam
billing ledger. This runbook operationalizes the **cutover maintenance window**
that the manifests README (`infra/k8s/production/postgres-ha/README.md`)
explicitly defers to RFC 0012 §5.3.

> [!IMPORTANT]
> Enclii-first doctrine: routine production ops go through Enclii. Direct
> `kubectl`/CNPG plugin use here is **platform-infra break-glass** for a
> stateful-data migration with no Enclii adapter yet. Record actor, window,
> commands, and result. File an adapter-gap note if Enclii should own any step.

## Preconditions (do not start the window until ALL are true)

- [ ] `postgres-ha` README pre-merge checklist is fully green (image digests
      pinned; `instances` target decided; `postgres-ha-superuser` Secret present).
- [ ] `cnpg-operator` Application is `Synced`/healthy; `postgres-ha` `Cluster`
      reports a primary + in-sync replica (`pg_stat_replication.sync_state = sync`).
- [ ] A manual CNPG `Backup` to `s3://enclii-backups/cnpg/postgres-ha/` has
      succeeded (validates the R2 pipeline).
- [ ] WAL archiving on the **old** instance is healthy (`docs/runbooks/POSTGRES_WAL_ARCHIVING.md`
      / internal-devops `runbooks/postgres-wal-archiving.md` report `wal: ok`),
      so a point-in-time fallback exists.
- [ ] Dual-write/replication soak completed per RFC 0012 §5.3 with replica lag
      within target for the agreed soak duration.
- [ ] Operator approval recorded on RFC 0012 §8 owner-action gate.
- [ ] Maintenance window announced; synthetic revenue probe + billing alerts ack'd.

## Cutover steps (maintenance window)

1. **Freeze writes.** Scale down / set read-only the heaviest writers first
   (Dhanam API last so billing is quiesced for the shortest time). Prefer
   `enclii deploy`-driven scale to zero where an adapter exists; otherwise
   break-glass `kubectl -n <ns> scale`.
2. **Final sync checkpoint.** Confirm logical/physical replication has drained:
   `kubectl -n data exec postgres-ha-1 -c postgres -- psql -U postgres -c \
   "SELECT now()-pg_last_xact_replay_timestamp() AS lag;"` → ~0.
3. **Repoint PgBouncer** from the old `data/postgres` Service to the
   `postgres-ha-rw` Service (the connection-target flip the README describes).
   Apply via the PgBouncer config manifest + reload (`docs/runbooks/CONFIG_RELOAD_RUNBOOK.md`).
4. **Verify writability + identity:**
   `psql "$PGBOUNCER_DSN" -c "SELECT inet_server_addr(); SELECT pg_is_in_recovery();"`
   → expect the new primary, `pg_is_in_recovery = f`.
5. **Smoke the revenue path** (most important): bring Dhanam API back, hit
   `GET https://api.dhan.am/health/full` (DB + Redis + queues green), then run
   the Dhanam staging-commercial / synthetic billing smoke against the new DB.
6. **Unfreeze** remaining writers in dependency order. Watch CNPG PodMonitor
   alerts (degraded, sync-replica-lost, multiple-writable-primaries).

## Rollback (if any verify step fails)

1. Repoint PgBouncer back to the old `data/postgres` Service + reload.
2. Re-enable writers; confirm `health/full` green on the old DB.
3. The old instance was never decommissioned during the window, so this is a
   connection-target flip, not a restore. Capture logs; do not retry until the
   failing precondition is understood.

## Post-cutover

- [ ] Observe ≥24h with CNPG alerts quiet and replica `sync`.
- [ ] Execute a failover drill (`internal-devops/runbooks/postgres-failover-drill.md`);
      log measured RPO/RTO in `docs/runbooks/RESTORE_DRILL_LOG.md`.
- [ ] Only after the drill passes, schedule decommission of the old
      single-instance `data/postgres` Deployment (separate change).
- [ ] Update First-Pesos roadmap blocker #1 to closed with evidence links.

## References

- Manifests + apply order: `infra/k8s/production/postgres-ha/README.md`
- Decision + cutover plan: `internal-devops/rfcs/0012-postgres-ha-via-cnpg.md` §5.3, §8
- Failover drill: `internal-devops/runbooks/postgres-failover-drill.md`
- DB recovery: `docs/runbooks/DATABASE_RECOVERY.md`
- WAL archiving: `docs/runbooks/POSTGRES_WAL_ARCHIVING.md`
