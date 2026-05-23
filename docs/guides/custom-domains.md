# Custom domains and TLS

Attach your own domain names to a service with automatic TLS (cert-manager). This is **Commercial GA bet B**.

## Workflow

1. **Add** the domain to the service and environment.
2. **Configure DNS** — CNAME to the tunnel/ingress target and TXT for verification.
3. **Verify** ownership (`enclii domains verify`).
4. **Deploy** — Junction routing and certificates are reconciled after verification.

## CLI

```bash
enclii domains list --service my-api
enclii domains add api.example.com --service my-api --env production
enclii domains status api.example.com --service my-api
enclii domains verify api.example.com --service my-api
enclii domains remove api.example.com --service my-api
```

See [domains CLI reference](../cli/commands/domains.md).

## UI

**Services → Networking / Domains** (or project networking views) for the same operations without the CLI.

## Zero Trust

Some domains support Cloudflare Zero Trust toggles via the API (`ToggleZeroTrust`); use the UI or API for policy changes.

## Staging proof

Use `DOMAIN_E2E_TOKEN`, `DOMAIN_E2E_SERVICE_ID`, and `DOMAIN_E2E_ENVIRONMENT_ID` (optional `DOMAIN_E2E_DOMAIN`). See [COMMERCIAL_GA_STAGING_PROOF.md](../production/COMMERCIAL_GA_STAGING_PROOF.md).

## Related

- [TESTING_GUIDE — Custom Domain & TLS](./TESTING_GUIDE.md#test-suite-3-custom-domain--tls-p1)
- [Railway migration — Custom domains](./RAILWAY_MIGRATION_GUIDE.md#custom-domains--routing)
