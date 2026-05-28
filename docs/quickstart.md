---
title: 5-Minute Quickstart
description: Deploy your first service to Enclii in 5 minutes
sidebar_position: 0
tags: [quickstart, getting-started, first-deploy]
---

# Deploy your first Enclii service in 5 minutes

This is the fastest path from zero to a live URL. You will install the CLI, sign in, scaffold a service, and deploy it.

> **Heads up:** Enclii currently onboards new tenants via an operator handoff. Once your account is provisioned (email `hello@enclii.dev`), the steps below take about 5 minutes. Fully self-serve signup ships with P3.2.

## 0. What you'll need (30 sec)

- A **GitHub account** (used for CLI sign-in).
- **Docker Desktop** installed, or any Docker-compatible runtime (`docker info` works).
- A **fresh terminal**.

## 1. Install the CLI (30 sec)

**Linux/macOS release archive:**
```bash
VERSION=v1.0.0-alpha.1
OS=linux   # use darwin for macOS
ARCH=amd64 # use arm64 on Apple Silicon or ARM Linux
curl -LO "https://github.com/madfam-org/enclii/releases/download/${VERSION}/enclii_${VERSION}_${OS}_${ARCH}.tar.gz"
tar -xzf "enclii_${VERSION}_${OS}_${ARCH}.tar.gz"
sudo install -m 0755 "enclii_${VERSION}_${OS}_${ARCH}/enclii" /usr/local/bin/enclii
```

**Linux / from source (any OS with Go 1.25+):**
```bash
git clone https://github.com/madfam-org/enclii.git
cd enclii
make install-cli           # builds + installs to /usr/local/bin/enclii
```

To install to a custom directory: `make install-cli CLI_INSTALL_DIR=$HOME/.local/bin`.

**Windows:** install via WSL2 and follow the Linux instructions above. A native PowerShell installer is on the roadmap — track [issue tracker](https://github.com/madfam-org/enclii/issues) for updates.

Verify:
```bash
enclii version
```

If `enclii: command not found`, restart your shell or add `$HOME/.local/bin` to `PATH`.

## 2. Sign in (30 sec)

```bash
enclii login
```

A browser opens to `auth.madfam.io` (Janua SSO). Sign in with GitHub. The CLI finishes the OAuth handshake and stores your token at `~/.enclii/config.yaml`.

**Expected:**
```
✓ Signed in as you@example.com
```

If the browser didn't open, copy the printed URL manually. If the callback times out, check your firewall or popup blocker — the CLI listens on a random localhost port during login.

## 3. Scaffold a service (1 min)

Pick any directory (or clone one of the [starter templates](./templates/templates.md)):

```bash
mkdir my-service && cd my-service
enclii init
```

Enclii detects your runtime and writes a `service.yaml` at the repo root.

**Expected:**
```
🚂 Initializing Enclii service 'my-service'...
Detected: Node.js (auto)
Created: service.yaml

Next: run `enclii deploy` to deploy this service.
```

Review `service.yaml`. The defaults are safe: port auto-detected, 2 replicas, `/health` probe, rolling deploys.

The `--template` flag accepts `auto`, `node`, `go`, or `python`. Named framework templates (`nextjs`, `fastapi`, `django`, …) are tracked in the [template catalog](./templates/templates.md).

## 4. Deploy (2 min)

```bash
enclii deploy
```

Default environment is `dev`. Add `--wait` if you want the CLI to block until the deploy is healthy.

**Expected output:**
```
→ Building image (buildpack auto-detect: Node.js)
→ Push to ghcr.io/<org>/my-service:<sha>
→ Release v1 created
→ Deploy to dev.my-service.enclii.dev
✓ Live at https://dev.my-service.enclii.dev
```

**If the build failed:** see [Build failures](./troubleshooting/build-failures.md). The most common cause is a missing `package.json` start script or a mis-detected runtime — add `spec.build.type: node` to `service.yaml`.

**If the deploy timed out:** see [Deployment issues](./troubleshooting/deployment-issues.md). Check that your app listens on `$PORT` (or `$ENCLII_PORT`) and that `/health` returns `200`.

## 5. See it live (30 sec)

```bash
curl https://dev.my-service.enclii.dev/health
# {"status":"ok"}
```

Open [app.enclii.dev](https://app.enclii.dev) to view the service in the dashboard: live logs, metrics, deploy history, and environment variables.

---

## Next: what to try

- **Tail logs:** `enclii logs my-service -f` → [logs command](./cli/commands/logs.md)
- **Promote to production:** `enclii deploy --env prod` → [deploy command](./cli/commands/deploy.md)
- **Add a custom domain:** `enclii domains add mydomain.com` → [domains command](./cli/commands/domains.md)
- **Set a secret:** `enclii secrets set DATABASE_URL "postgresql://..."` → [secrets command](./cli/commands/secrets.md)
- **Roll back:** `enclii rollback my-service` → [rollback command](./cli/commands/rollback.md)

## Migrating from another platform?

- [From Vercel →](./guides/migrating-from-vercel.md)
- [From Railway →](./guides/migrating-from-railway.md)
- [From Heroku →](./guides/migrating-from-heroku.md)

## Running your own cluster?

Enclii is self-hostable end-to-end. See [Self-hosting Enclii](./guides/SELF_HOSTING.md) and [Infrastructure](./infrastructure/README.md) for the bootstrap path.
