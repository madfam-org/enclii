# Dispatch topology inventory

Dispatch's `/topology` page reads `/api/topology`, which forwards the signed-in
caller's token to Switchyard's read-only `POST /v1/ops/inventory/topology` adapter.
Switchyard owns Kubernetes access and checks the server-resolved platform-admin
rank. Dispatch does not query Kubernetes directly for this page, use a service
credential fallback, or cache the server response across callers.

The API's explicit `ENCLII_PLATFORM_ADMIN_EMAILS` configuration is reconciled at
API startup. Keep the production environment component and Deployment patch in
agreement. A tenant admin role or token scope does not grant this rank. An
operator allow-list correction needs an API rollout before the rank can change;
a browser reload alone cannot apply it. See the
[tenant-scope rollout procedure](TENANT_SCOPE_ENFORCEMENT_ROLLOUT.md).

## What the graph means

Nodes and namespace placements come from observed Kubernetes objects. Builder
roles recognize both `role=builder` and `enclii.dev/role=builder` in Switchyard.
Pending pods remain in inventory and namespace phase counts even when they have
no assigned node.

CPU and memory bars represent regular-container requests of running pods against
node allocatable resources. They are not live utilization or complete scheduler
reservations: pending pods, init containers and pod overhead are excluded. The
node pod counter also counts running pods; the drawer lists observed pods in all
phases. Do not use these bars alone to justify increasing runner concurrency.

## Release verification

1. Require the full current-head CI matrix, including Dispatch tests, before
   merging. Check that the signed Dispatch image was pinned and the Enclii
   application reached the actual release revision.
2. If the operator allow-list changed, verify the Switchyard API rollout too.
   Confirm inventory access as each explicitly authorized operator. Do not
   substitute a service key for a refused user.
3. Open `/topology` through the normal sign-in flow. Confirm nonempty nodes,
   correct roles, observed namespaces, node/pod drawers and Refresh. Compare
   counts with Enclii inventory at a similar timestamp.
4. Verify that authentication and rank refusals remain 401/403. Other unavailable,
   malformed or unconfigured adapter responses must remain errors, rather than
   appear as a healthy empty graph. Private transport details must not be shown.
5. A failed refresh may retain the last successful snapshot with a visible stale
   warning. A 401/403 clears that snapshot. Recheck anonymous denial and logout.

Local checks build the shared workspace packages before Dispatch type checking
and production build, as its Dockerfile does. Tests mock the inventory boundary;
passing tests do not constitute production rollout or browser evidence.
