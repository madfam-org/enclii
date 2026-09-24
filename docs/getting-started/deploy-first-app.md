# Deploy Your First App on Enclii

Get your application running on Enclii in under 5 minutes.

## Prerequisites

- A GitHub account
- A web application (Node.js, Go, Python, or any language)
- A terminal with shell access

---

## Step 1: Install the CLI

### Linux/macOS release archive

```bash
VERSION=v1.0.0-alpha.12
OS=linux   # use darwin for macOS
ARCH=amd64 # use arm64 on Apple Silicon or ARM Linux
curl -LO "https://github.com/madfam-org/enclii/releases/download/${VERSION}/enclii_${VERSION}_${OS}_${ARCH}.tar.gz"
tar -xzf "enclii_${VERSION}_${OS}_${ARCH}.tar.gz"
sudo install -m 0755 "enclii_${VERSION}_${OS}_${ARCH}/enclii" /usr/local/bin/enclii
```

### Linux / from source (any OS with Go 1.26+)

```bash
git clone https://github.com/madfam-org/enclii.git
cd enclii
make install-cli           # builds + installs to /usr/local/bin/enclii
```

### Verify Installation

```bash
enclii version
```

---

## Step 2: Authenticate

```bash
enclii login
```

This opens your browser to sign in through Janua SSO. Confirm the account with:

```bash
enclii whoami
```

---

## Step 3: Initialize Your Service

Navigate to your project directory and initialize Enclii:

```bash
cd my-app
enclii init --template express   # or another catalog slug; the default is auto
```

This writes a starter `service.yaml` (it fails if one already exists). `init` does not inspect your code: the service and project name default to the directory name, and the generated port and env block are placeholders. Review it:

```yaml
apiVersion: enclii.dev/v1alpha
kind: Service
metadata:
    name: my-app
    project: my-app
spec:
    build:
        type: express
    runtime:
        port: 8080
        replicas: 2
        healthCheck: /health
    env:
        - name: NODE_ENV
          value: production
```

Set `runtime.port` to the port your app listens on (for example `3000`). See [`enclii init`](../cli/commands/init.md) for the template catalog.

---

## Step 4: Deploy to Development

Commit your code, then deploy. Without `--env`, `enclii deploy` targets the `dev` environment:

```bash
enclii deploy --wait
```

The command must run inside a git repository. It builds the current commit, creates the project, service, and environment if they do not exist, deploys with a rolling update, and with `--wait` polls until the deployment is healthy. See [`enclii deploy`](../cli/commands/deploy.md).

---

## Step 5: View Logs

Stream logs from your running service:

```bash
enclii logs my-app -f
```

Press `Ctrl+C` to stop streaming. Without `-f` it prints the last 100 lines (`-n` to change).

---

## Step 6: Check Status

View your running services:

```bash
enclii ps
```

---

## Step 7: Deploy to Production

When ready, deploy to production:

```bash
enclii deploy --env production --wait
```

For a gradual rollout, deploy as a canary instead: `enclii deploy --env production --canary 10 --change-ticket <url>`.

---

## Next Steps

### Add a Custom Domain

```bash
# Add your domain
enclii domains add api.example.com --env production

# Verify DNS
enclii domains verify api.example.com
```

### Set Up Environment Variables

```bash
# Add a secret (encrypted, masked)
enclii secrets set DATABASE_URL="postgresql://..." --secret --env production

# Add a regular variable
enclii secrets set LOG_LEVEL=info --env production
```

### Configure Auto-Deploy

Declare the repository and branch in your `service.yaml`:

```yaml
spec:
  build:
    source:
      git:
        repository: https://github.com/<org>/<repo>
        branch: main
        autoDeploy: true
```

Then sync (`--reconcile-existing` updates a service that is already registered):

```bash
enclii services-sync --dir . --project <project-slug> --reconcile-existing
```

Now every push to `main` builds and deploys automatically. `services-sync` sets the auto-deploy environment to `production`; the CLI has no flag to choose another.

### Set Up Preview Environments

Enable preview environments for pull requests. When you open a PR, Enclii automatically:

1. Builds your branch
2. Deploys to `pr-123.preview.enclii.app`
3. Comments on your PR with the preview URL
4. Cleans up when the PR is merged/closed

Configure in the dashboard or via GitHub App integration.

---

## Common Operations

### Rollback a Deployment

```bash
enclii rollback my-app
# Rolls back to previous version
```

### Scale Your Service

Update replicas in `service.yaml`:

```yaml
spec:
  runtime:
    replicas: 3
```

Then deploy (`enclii deploy` reads `service.yaml` from the current directory; pass `-f` for another path):

```bash
enclii deploy --env production
```

### View Build Status

```bash
# Recent builds with status, and the error message of failed builds
enclii releases my-app
```

---

## Example Projects

### Node.js/Express

```javascript
// server.js
const express = require('express');
const app = express();
const port = process.env.ENCLII_PORT || 3000;

app.get('/health', (req, res) => {
  res.json({ status: 'ok' });
});

app.get('/', (req, res) => {
  res.send('Hello from Enclii!');
});

app.listen(port, () => {
  console.log(`Server running on port ${port}`);
});
```

### Go

```go
// main.go
package main

import (
    "net/http"
    "os"
)

func main() {
    port := os.Getenv("ENCLII_PORT")
    if port == "" {
        port = "8080"
    }

    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte(`{"status":"ok"}`))
    })

    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello from Enclii!"))
    })

    http.ListenAndServe(":"+port, nil)
}
```

### Python/Flask

```python
# app.py
from flask import Flask, jsonify
import os

app = Flask(__name__)

@app.route('/health')
def health():
    return jsonify(status='ok')

@app.route('/')
def hello():
    return 'Hello from Enclii!'

if __name__ == '__main__':
    port = int(os.environ.get('ENCLII_PORT', 8000))
    app.run(host='0.0.0.0', port=port)
```

---

## Troubleshooting

### Build Fails

```bash
# Recent builds with status and the error message of failed builds
enclii releases my-app

# Common issues:
# - Missing package.json scripts
# - Invalid Dockerfile
# - Missing dependencies
```

### Health Check Fails

Ensure your app:
1. Listens on `$ENCLII_PORT` (or the port in your config)
2. Responds to `/health` with a 200 status
3. Starts within the `initialDelaySeconds` timeout

```bash
# Check health endpoint locally
curl http://localhost:3000/health
```

### Deployment Stuck

```bash
# Check service status
enclii ps --env production
enclii deploy ls my-app

# View deployment logs
enclii logs my-app --since 10m
```

---

## Getting Help

- **Documentation**: [docs.enclii.dev](https://docs.enclii.dev)
- **CLI Help**: `enclii --help` or `enclii <command> --help`
- **GitHub Issues**: [github.com/madfam-org/enclii/issues](https://github.com/madfam-org/enclii/issues)

---

## See Also

- [CLI Reference](../cli/README.md)
- [Service Specification](../reference/service-spec.md)
- [GitHub Integration](../integrations/github.md)
- [Cloudflare Integration](../infrastructure/CLOUDFLARE.md) — custom domain and tunnel route setup
