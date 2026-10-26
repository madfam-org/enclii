# Karafiel production enrollment diagnostics

The existing human-operator endpoint
`POST /v1/admin/projects/karafiel/environments/production/target-enrollment`
adds two bounded fields to a failed **dry** response:

```json
{
  "operation": "ops.apps.bind-target",
  "status": "refused",
  "error": "actual workload ownership/source could not be verified",
  "refusal_code": "ownership_unverified",
  "diagnostic_stage": "snapshot",
  "diagnostic_reason": "parent_read"
}
```

`diagnostic_stage` is `snapshot` for an existing snapshot-read gate or
`validation` for an existing ownership/source validation gate. The reason is
a closed code for the first failed gate. It contains no observed value,
resource identity, source hash, configuration or upstream error text. The
complete whitelist is in
[`karafiel_production_diagnostics.go`](../../apps/switchyard-api/internal/workloadtargets/karafiel_production_diagnostics.go).

There is no diagnostic request flag, additional permission, extra resource
read or mutation. HTTP statuses and the existing refusal fields retain their
meaning. Authentication and malformed-request refusals are unchanged. A
successful dry plan, plan fingerprint, apply, replay or commit-unknown response
is unchanged. Diagnostic data never enters a persisted binding or audit event.

A refusal remains a hold. A code identifies a failed check; it does not prove
that other checks passed or authorize a retry, metadata repair, source change,
image promotion or Kubernetes operation. Inspect fresh supported readbacks,
resolve the cause through the reviewed native path, then obtain a new complete
dry plan before separately considering apply. Enrollment remains metadata
and audit only; actual runtime readiness and the held release configuration
require separate acceptance.
