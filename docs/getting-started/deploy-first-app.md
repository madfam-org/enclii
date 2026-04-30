# Deploy Your First App on Enclii

Get your application running on Enclii in under 5 minutes.

## Prerequisites

- A GitHub account
- A web application (Node.js, Go, Python, or any language)
- A terminal with shell access

---

## Step 1: Install the CLI

### macOS

```bash
brew install enclii/tap/enclii
```

### Linux

```bash
curl -sSL https://get.enclii.dev | bash
```

### Verify Installation

```bash
enclii version
# Output: Enclii CLI v0.5.x
```

---

## Step 2: Authenticate

```bash
enclii login
```

This opens your browser to sign in with GitHub. Once authenticated, you'll see:

```
✓ Logged in as developer@example.com
```

---

## Step 3: Initialize Your Service

Navigate to your project directory and initialize Enclii:

```bash
cd my-app
enclii init
```

**Output:**
```
Detected: Node.js application (package.json found)
Created: enclii.yaml

Service: my-app
Type:    http
Port:    3000
Build:   nixpacks (auto-detected)

Next: Run `enclii deploy` to deploy
```

This creates an `enclii.yaml` configuration file. Review it:

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: my-app
  project: my-project
spec:
  build:
    type: auto
  runtime:
    port: 3000
    replicas: 1
    healthCheck: /health
  env:
    - name: NODE_ENV
      value: production
```

---

## Step 4: Deploy to Preview

Deploy your app to a preview environment:

```bash
enclii deploy
```

**Output:**
```
Building service...
  Detected: Node.js (nixpacks)
  Building: ████████████████████ 100%
  Image: ghcr.io/madfam-org/my-app:v1.0.0

Creating release...
  Release: rel_abc123
  Commit:  a1b2c3d (Initial commit)

Deploying to preview...
  Progress: ████████████████████ 100%

✓ Deployment successful!
  URL: https://my-app-preview.enclii.app
  Status: healthy
```

Visit the URL to see your running application.

---

## Step 5: View Logs

Stream logs from your running service:

```bash
enclii logs my-app -f
```

**Output:**
```
2025-01-11T10:30:15Z [INFO]  Server started on port 3000
2025-01-11T10:30:16Z [INFO]  Connected to database
2025-01-11T10:31:02Z [INFO]  GET / 200 45ms
```

Press `Ctrl+C` to stop streaming.

---

## Step 6: Check Status

View your running services:

```bash
enclii ps
```

**Output:**
```
NAME     ENV       STATUS    INSTANCES   CPU   MEMORY   URL
my-app   preview   running   1/1         12%   156Mi    https://my-app-preview.enclii.app
```

---

## Step 7: Deploy to Production

When ready, deploy to production:

```bash
enclii deploy --env production
```

**Output:**
```
Deploying to production...
  Strategy: rolling
  Progress: ████████████████████ 100%

✓ Deployment successful!
  URL: https://my-app.enclii.app
  Status: healthy
```

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
# Add a secret
enclii secrets set DATABASE_URL "postgresql://..." --env production

# Add a regular variable
enclii env set LOG_LEVEL "info" --env production
```

### Configure Auto-Deploy

Update your `enclii.yaml`:

```yaml
spec:
  autoDeploy:
    enabled: true
    branch: main
    environment: staging
```

Then sync:

```bash
enclii services sync
```

Now every push to `main` automatically deploys to staging.

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

Update replicas in `enclii.yaml`:

```yaml
spec:
  runtime:
    replicas: 3
```

Then sync and deploy:

```bash
enclii services sync
enclii deploy --env production
```

### View Build Logs

```bash
enclii builds logs --latest
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
# View build logs
enclii builds logs --latest

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
# Check pod status
enclii ps --wide

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
