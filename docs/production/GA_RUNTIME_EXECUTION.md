# GA Runtime Execution

This document points operators to the private runtime execution pack and records the Enclii-side command surfaces used for Stability GA and Commercial GA.

Private execution pack: `internal-devops/runbooks/ga-ops-execution-pack.md`

## Canonical Enclii command surfaces

| Area | Command surface | State |
|---|---|---|
| Capabilities | `enclii ops capabilities`, `enclii providers capabilities` | Read-only |
| Vault readiness | `enclii vault status`, `enclii ops secrets vault` | Read-only |
| ExternalSecret sync | `enclii secrets sync <name> --namespace <ns>` | Apply-capable with reason |
| Secret rotation | `enclii secrets rotate <target>` | Plan-first only |
| Cloudflare DNS | `enclii providers cloudflare dns-apply <domain>` | Apply-capable when provider configured |
| Cloudflare credentials | `enclii providers cloudflare credentials` | Contract-read surface |
| Storage | `enclii ops storage longhorn`, `enclii ops storage volumes` | Read/plan surface |
| Argo apps | `enclii ops apps status`, `enclii ops apps diff`, `enclii ops apps sync` | Sync apply-capable with reason |
| Restore drills | `enclii ops jobs list`, `enclii ops jobs trigger` | Trigger apply-capable with reason |

## Evidence routing

- Open task state belongs in `REMAINING_OPS_GA.md`.
- Sign-off state belongs in `GA_READINESS_SCORECARD.md`.
- Commercial launch state belongs in `COMMERCIAL_GA_TRACKER.md`.
- Adapter gaps belong in `docs/ADAPTER_GAPS.md`.
