# Kalya feed credential custody

The control plane provisions independent whole-tenant read credentials for each
consumer. Kalya owns token validity and stores SHA-256 hashes only. Enclii
owns generation and durable Vault custody; plaintext never enters an operator
response, a log, a Kalya request, or the database.

## Release order

1. Deploy the `kalya/internal-api-key` intake target. Generate `internal_api_key`
   through server-side secret intake and verify its Vault write.
2. Enable Kalya's dedicated `kalya-internal-api-key` ExternalSecret after that
   write. Deploy the native `PUT /api/v1/internal/feed-tokens` custody endpoint.
3. Import and reconcile the tenant's schedules before publishing its feeds.
4. Preview `enclii secrets provision kalya-feed --tenant <slug> --consumers crea-map,nauta`.
   Apply with an audit reason after reviewing the tenant and consumer names.
5. Enable each consumer's isolated ExternalSecret once its Vault projection is
   present. Verify ESO synchronization, pod rollout and the authenticated feed.
6. Re-run the dry-run. A completed operation proposes no credential writes.

A missing optional ExternalSecret property can fail an Argo sync even if the
pod reference is optional. Stage resource definitions outside kustomization
until custody exists. Never merge a missing property into the database/login
ExternalSecret.

## Retry and rotation protocol

Each consumer Vault path holds `kalya_feed_custody_<tenant>` state, alongside its
projected feed properties. ESO does not project this state. CAS mutations reread
and recompute updates after concurrent writes, preserving other tenant entries.

1. **Pending:** generate 32 random bytes and persist them in Vault first.
2. **Prepare:** register only the hash in Kalya under a tenant/consumer label.
   Repeated prepare requests find the same hash; they never issue another token.
3. **Publish:** merge the feed URL or named workspace token into the consumer's
   Vault path and mark the custody record published.
4. **Finalize:** retire only the explicit predecessor hash. For rotation, Kalya
   requires evidence the new credential was used before retiring the old one,
   so the asynchronous ESO/Reloader window keeps the previous feed working.
5. **Active:** acknowledge completion in custody. A failed acknowledgement can
   safely repeat prepare and finalize without creating another credential.

A partial result names the failed phase without credential material. Retry the
same command; pending state is reused. If retirement is awaiting observation,
finish consumer projection/rollout, exercise the authenticated feed, then retry.
The old credential remains valid until that point. Kalya admits at most two
live credentials per provisioned label during an unfinished rotation.

Explicit rotation requires `--rotate --idempotency-key <stable-operation-id>`.
Use the same key on retries. Reusing a completed key performs no generation.
A new key requests a new rotation. Other consumers and tenant map entries are
preserved. Legacy credentials outside the provisioner's distinct consumer label
are not revoked implicitly; their ownership must be established separately.
