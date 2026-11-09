# Service backend evidence through Enclii

The platform-rank inventory request `POST /v1/ops/inventory/topology` retains its
existing node, namespace, pod and Service fields. It adds read-only context for
investigating a new connection that fails while a serving application is Ready.
A readiness query can reuse an established database connection; it does not
prove a newly created observer can connect.

Each namespace returns `data_access_label` and `postgres_consumer_label` for the
two existing Enclii namespace-consumer selectors. Values are bounded to `true`,
`false`, `absent` or `other`. No arbitrary namespace label values are exported,
and the read changes no labels, role authority or network policy.

Each Service has `backend_observation`:

- `selector_matched_pods` and `selector_matched_ready_pods` come from the pod
  inventory in the exact namespace. A selector match is context, not a route.
- `listed_slices`, actual slice `ports`, and ready/not-ready/unknown/terminating
  address counts come from a complete EndpointSlice list. Counts deduplicate
  addresses; conflicting states become one unknown. A missing readiness
  condition stays unknown. Address values are not returned.
- Slice association requires exact namespace, Service name label and Service
  owner name/UID/API version. A recreated or unverified owner produces
  `unverified_owner` with null address counts and ports.
- `permission_denied`, `timeout`, `incomplete` and `unavailable` keep counts and
  ports null. They never become an observed zero. Legacy topology remains
  available when this additional read fails.
- `observed` with zero slices/addresses means the complete read found none for
  that Service. It still does not prove TCP, TLS, authentication, SQL visibility,
  read-only semantics or absence of writers.

Switchyard needs only the added `list` permission for
`discovery.k8s.io/endpointslices`. No endpoint write, pod exec, credential read,
label mutation or network exception is added. Use the same operator API and
reviewed observation path to collect actual transport and query evidence.
Never restart a healthy serving workload merely to test a new connection.

**Boundary checkpoint — 2026-10-19; owner: platform maintainer.** This runbook
contains public-safe API behavior and proof limits. Runtime topology, identities,
transport failures and release receipts stay in private internal-devops. Policy:
[public repository boundary](../PUBLIC_REPO_BOUNDARY.md).
