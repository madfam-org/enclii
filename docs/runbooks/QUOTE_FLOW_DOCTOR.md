# Quote-flow doctor

`enclii quote-flow verify` is the Enclii-first readiness check for the Selva -> Yantra4D -> Cotiza -> ForgeSight quote path.

```sh
enclii quote-flow verify --project tablaco --agent selva --require-market-verified
```

The command calls Switchyard API's admin-only operation contract:

```text
POST /v1/ops/quote-flow/verify
```

It does not use `kubectl`, exec into containers, or require direct production access. The API reports:

- Selva worker auth/token readiness.
- Selva worker endpoint readiness.
- Yantra4D project endpoint readiness for the requested project.
- Cotiza import health.
- ForgeSight pricing/market-data readiness.
- Overall disposition: `client_ready`, `review_only`, `blocked_by_auth`, `blocked_by_market_data`, or `blocked_by_unhealthy_infrastructure`.

Use `--json` for automation. Endpoint overrides are available with `--selva-url`, `--yantra-url`, `--cotiza-url`, and `--forgesight-url` while upstream contracts stabilize.
