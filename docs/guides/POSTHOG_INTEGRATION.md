# PostHog Analytics Integration Guide

How to add PostHog product analytics to any Madfam ecosystem application.

## Architecture

All analytics traffic is routed through `analytics.enclii.dev`, a Cloudflare reverse proxy that forwards to the PostHog ingestion API. This avoids ad-blocker interference and keeps data flows within Enclii's infrastructure boundary.

```
Browser / Go service
    |
    v
analytics.enclii.dev  (Cloudflare Worker / tunnel route)
    |
    v
PostHog Cloud or Self-Hosted
```

## Prerequisites

| Item | Value |
|------|-------|
| PostHog project API key | `phc_...` (get from PostHog project settings) |
| Ingestion endpoint | `https://analytics.enclii.dev` |

---

## Frontend (Next.js / React)

### 1. Install the SDK

```bash
pnpm add posthog-js
```

### 2. Set environment variables

Add to `.env.local` (never commit real keys):

```env
NEXT_PUBLIC_POSTHOG_KEY=phc_your_project_key
NEXT_PUBLIC_POSTHOG_HOST=https://analytics.enclii.dev
```

### 3. Add the provider

In your root layout or providers file, wrap the app:

```tsx
// app/providers.tsx (or wherever your provider tree lives)
import { PostHogProvider } from "@/lib/analytics/PostHogProvider";

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <PostHogProvider>
      {/* ...other providers... */}
      {children}
    </PostHogProvider>
  );
}
```

If you are in a non-Enclii app that does not have the provider component, you can initialize directly:

```ts
import posthog from "posthog-js";

posthog.init("phc_your_project_key", {
  api_host: "https://analytics.enclii.dev",
  capture_pageview: true,
  autocapture: true,
  respect_dnt: true,
  persistence: "localStorage+cookie",
  secure_cookie: true,
  disable_session_recording: true,
});
```

### 4. Track events

```ts
import { trackEvent, identifyUser, resetUser } from "@/lib/analytics";

// After login
identifyUser(user.id, { email: user.email, org: user.orgId });

// Custom events
trackEvent("project.created", { project_id: "proj_abc" });
trackEvent("deployment.triggered", { env: "production", strategy: "canary" });

// On logout
resetUser();
```

### Do-Not-Track

The provider respects the browser `navigator.doNotTrack` signal. When DNT is set to `"1"`, PostHog will not be initialized and no data is collected.

---

## Go Backend

### 1. Add the dependency

```bash
cd apps/switchyard-api   # or your Go module root
go get github.com/posthog/posthog-go
```

### 2. Set environment variables

```env
ENCLII_POSTHOG_API_KEY=phc_your_project_key
ENCLII_POSTHOG_ENDPOINT=https://analytics.enclii.dev
```

### 3. Initialize the client

```go
import "github.com/madfam-org/enclii/apps/switchyard-api/internal/analytics"

// In main.go or service initialization:
phClient := analytics.New(cfg.PostHogAPIKey, cfg.PostHogEndpoint, logrus.StandardLogger())
defer phClient.Close()
```

### 4. Track events

```go
import "github.com/posthog/posthog-go"

// After a deployment completes
phClient.Track(userID, "deployment.completed", posthog.NewProperties().
    Set("project_id", project.ID).
    Set("environment", "production").
    Set("strategy", "canary").
    Set("duration_ms", elapsed.Milliseconds()))

// Identify a user on login / signup
phClient.Identify(userID, posthog.NewProperties().
    Set("email", user.Email).
    Set("org_id", user.OrgID).
    Set("plan", user.Plan))

// B2B group analytics
phClient.GroupIdentify("organization", orgID, posthog.NewProperties().
    Set("name", org.Name).
    Set("plan", org.Plan).
    Set("member_count", org.MemberCount))

// Feature flags
if phClient.IsFeatureEnabled(userID, "new-dashboard") {
    // serve new experience
}
```

### Graceful degradation

When `ENCLII_POSTHOG_API_KEY` is empty, the client is created in disabled mode. All methods (`Track`, `Identify`, `Close`, etc.) become safe no-ops. No conditional checks needed at call sites.

---

## Recommended Event Taxonomy

Use a `noun.verb` naming convention for consistency across frontend and backend.

| Event | Emitter | Properties |
|-------|---------|------------|
| `project.created` | API | `project_id`, `org_id` |
| `project.deleted` | API | `project_id`, `org_id` |
| `deployment.triggered` | API | `project_id`, `env`, `strategy` |
| `deployment.completed` | API | `project_id`, `env`, `duration_ms`, `status` |
| `deployment.rolled_back` | API | `project_id`, `env`, `reason` |
| `build.started` | Roundhouse | `project_id`, `builder` |
| `build.completed` | Roundhouse | `project_id`, `duration_ms`, `image_size` |
| `service.scaled` | API | `service_id`, `from`, `to` |
| `user.signed_up` | UI | `auth_provider` |
| `user.logged_in` | UI | `auth_provider` |
| `page.viewed` | UI (auto) | `$current_url` |
| `button.clicked` | UI (auto) | `$element_id`, `$element_text` |

---

## Kubernetes Secret

For production deployments, add the API key to the service's K8s secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: switchyard-secrets
  namespace: enclii
type: Opaque
stringData:
  ENCLII_POSTHOG_API_KEY: "phc_your_project_key"
```

For frontend apps deployed as containers, set the env var at build time (since `NEXT_PUBLIC_` vars are inlined by Next.js):

```yaml
env:
  - name: NEXT_PUBLIC_POSTHOG_KEY
    valueFrom:
      secretKeyRef:
        name: switchyard-ui-secrets
        key: NEXT_PUBLIC_POSTHOG_KEY
```

---

## Cloudflare Reverse Proxy (analytics.enclii.dev)

Route all PostHog traffic through a Cloudflare Worker or tunnel route so that:

1. Ad-blockers do not drop events (first-party domain).
2. No third-party cookies are set.
3. Data stays within Enclii's infrastructure boundary for compliance.

Example tunnel route addition in `cloudflared-unified.yaml`:

```yaml
- hostname: analytics.enclii.dev
  service: https://us.i.posthog.com   # or your self-hosted PostHog URL
  originRequest:
    noTLSVerify: false
```

---

## Testing

In test environments, set `NEXT_PUBLIC_POSTHOG_KEY` to an empty string or omit it entirely. The SDK will not initialize and no events will be sent.

For the Go backend, pass an empty API key to `analytics.New()` and the client operates in disabled mode.
