# Argo sync submission and migration safety

## Submission contract

Enclii `ops apps sync` and `ops apps sync-sweep` submit a complete operation using
an atomic JSON Patch. The first instruction tests the Application's observed
`metadata.resourceVersion`; subsequent instructions preserve existing annotations
and replace `/operation` in full. A concurrent Argo operation or metadata change
causes a conflict response, never a merge into the running operation. Missing
resource versions fail closed. The adapter does not retry a conflicting write.

`already_running` means an operation was present when read. `state_changed` means
the atomic precondition or API validation rejected submission. Refresh status,
wait for an active operation to finish, then deliberately retry. Audit annotations
are part of the same transaction and are not written when the test fails.

This replaces a check-then-JSON-Merge-Patch sequence that could inherit a
concurrent self-heal's `sync.resources` and `initiatedBy.automated` fields. A
request intended as a full sync could consequently become selective.

## Migration limitation

[Argo CD 3.2 hook documentation](https://argo-cd.readthedocs.io/en/release-3.2/user-guide/sync-waves/)
states that selective sync does not run hooks. The
[3.2.5 controller](https://github.com/argoproj/argo-cd/blob/v3.2.5/controller/appcontroller.go#L2042-L2069)
constructs a selective resource list for self-heal retries. This happens even
without `ApplyOutOfSyncOnly=true`. A Deployment can therefore become healthy
without a `PreSync` migration job having run for that image.

The submission fix does **not** change Argo's native self-heal behavior or make
Deployment health proof of schema compatibility. Keep `PreSync` migration jobs,
but verify their exact image, completion and expected database migration ledger
before declaring a migration-bearing release complete. For durable protection
against every selective rollout path, add a same-image startup/init schema check
in the application that refuses to serve until its required migrations exist.
That application safeguard is follow-up work, not implemented by this adapter
change. Do not disable platform self-heal globally as a substitute.

## Reviewed rollout sequence

1. Release this adapter through normal review, CI and Enclii deployment. A local
   passing test or committed branch does not mean the fix is live.
2. Confirm the intended Git revision and migration image agree. Review migration
   compatibility, backup/restore evidence and expected ledger entries.
3. Inspect status and request a full application sync through Enclii:

   ```sh
   enclii ops apps status "$APP" --namespace argocd --json
   enclii ops apps sync "$APP" --namespace argocd --json
   enclii ops apps sync "$APP" --namespace argocd --apply \
     --reason "Reviewed migration-bearing release; verify full hook execution" --json
   ```

4. Inspect operation and hook evidence, not only `Healthy` / `Synced`. The
   requested operation must have no selective `sync.resources` list. Confirm the
   migration job used the release image and succeeded; confirm the required
   database migrations are applied. A later self-heal can replace the latest
   operation status, so retain the full-sync and job evidence for the release.
5. If the schema is behind, stop release completion. Use the application's
   documented native migration procedure through Enclii. Where Enclii lacks the
   required runtime adapter, record the gap and use the approved, documented
   runtime fallback. Never edit migration history to manufacture completion.

Do not copy production identifiers, raw infrastructure metadata or credentials
into this public runbook. Keep incident-specific evidence in `internal-devops`
or the protected operator handoff.
