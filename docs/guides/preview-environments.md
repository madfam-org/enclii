# Preview environments

PR-based preview deployments give each pull request a unique URL, build, and ephemeral environment. This is **bet A** in the [GA launch program](../production/COMMERCIAL_GA_TRACKER.md).

## How it works

1. Link the service to a GitHub repository (`git_repo` on the service).
2. GitHub sends `pull_request` webhooks to Enclii (project integration).
3. On `opened` / `synchronize`, Enclii creates a preview record, triggers a build, and deploys to a subdomain such as `pr-42-my-api.preview.enclii.app`.
4. On `closed` / `merged`, the preview is closed and resources are cleaned up.

## UI

Open **Services → &lt;service&gt; → Previews** to list active previews, open URLs, wake sleeping previews, or close/delete records.

## CLI

```bash
enclii previews list
enclii previews get --pr 42 --service my-api
enclii previews close <preview-id>
enclii previews wake <preview-id>
```

See [previews CLI reference](../cli/commands/previews.md).

## API

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/v1/services/:id/previews` | List previews (`?pr_number=` optional) |
| `POST` | `/v1/previews` | Create preview (manual / E2E) |
| `GET` | `/v1/previews/:id` | Get one preview |
| `POST` | `/v1/previews/:id/close` | Close preview |
| `POST` | `/v1/previews/:id/wake` | Wake sleeping preview |
| `DELETE` | `/v1/previews/:id` | Delete closed/failed preview |

## Staging proof

After deploying `main`, run lifecycle tests with `PREVIEW_E2E_TOKEN` and `PREVIEW_E2E_SERVICE_ID`. See [COMMERCIAL_GA_STAGING_PROOF.md](../production/COMMERCIAL_GA_STAGING_PROOF.md).

## Related

- [Webhook setup](./WEBHOOK_SETUP_GUIDE.md)
- [Migrating from Heroku — Review Apps](./migrating-from-heroku.md#review-apps--preview-environments)
