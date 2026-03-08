# WebSocket Services on Enclii

WebSocket services work out of the box on Enclii. No special configuration is needed — Cloudflare Tunnel transparently forwards WebSocket upgrade requests.

## How It Works

```
Client → Cloudflare Edge → Cloudflare Tunnel (cloudflared) → nginx-ingress → Pod
```

1. **Cloudflare Tunnel** proxies all HTTP traffic, including WebSocket upgrades (`Connection: Upgrade`, `Upgrade: websocket`). No tunnel-level configuration is needed.
2. **nginx-ingress** natively supports WebSocket connections. The HTTP/1.1 Upgrade handshake passes through to your service.
3. Your WebSocket server receives the upgraded connection as if it were directly connected.

## Service Configuration

WebSocket services use the same `enclii.yaml` as any HTTP service. Point `runtime.port` to your WebSocket server's HTTP port:

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: my-ws-service
  project: my-project
spec:
  domains:
    - name: ws.example.com
      environment: production
  runtime:
    port: 6001
```

### Health Checks

WebSocket servers must expose an HTTP health check endpoint (not a WebSocket endpoint). Kubernetes probes use HTTP GET requests:

```yaml
spec:
  runtime:
    port: 6001
    healthCheck: /health  # Must respond to HTTP GET with 200
```

Most WebSocket frameworks (Soketi, Socket.IO, ws) serve HTTP and WebSocket on the same port.

## Soketi Deployment Example

[Soketi](https://docs.soketi.app/) is a Pusher-compatible WebSocket server. See `examples/websocket-service.yaml` for a complete example.

Key environment variables for Soketi:

| Variable | Description | Example |
|----------|-------------|---------|
| `SOKETI_DEFAULT_APP_ID` | Application ID | `nuit` |
| `SOKETI_DEFAULT_APP_KEY` | Application key | `app-key` |
| `SOKETI_DEFAULT_APP_SECRET` | Application secret | (use Enclii secrets) |
| `SOKETI_PORT` | Listen port | `6001` |

## Custom Response Headers

Use the `headers` field to add custom HTTP response headers. This is useful for CORS or security headers like COOP/COEP:

```yaml
spec:
  headers:
    Access-Control-Allow-Origin: "https://app.example.com"
    Cross-Origin-Opener-Policy: "same-origin"
    Cross-Origin-Embedder-Policy: "require-corp"
```

Headers are injected via nginx ingress `configuration-snippet` annotations.

## Sticky Sessions

For multi-replica WebSocket deployments where the server doesn't share state across instances, you may need sticky sessions. This is not yet a first-class Enclii feature, but can be achieved with nginx ingress annotations if needed.

For single-replica deployments (common for Soketi), sticky sessions are not required.

## Monitoring and Verification

### Test with wscat

```bash
# Install wscat
npm install -g wscat

# Test WebSocket connection
wscat -c wss://ws.example.com/app/YOUR_APP_KEY
```

### Browser DevTools

1. Open **Network** tab
2. Filter by **WS** (WebSocket)
3. Look for the `101 Switching Protocols` response
4. Inspect frames in the **Messages** sub-tab

### Check nginx-ingress Logs

```bash
# View ingress controller logs for WebSocket upgrades
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx --tail=100 | grep upgrade
```

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| Connection drops after 60s | Cloudflare idle timeout | Send periodic ping frames (most WS libraries do this automatically) |
| `400 Bad Request` | Upgrade header not forwarded | Verify the domain is routed through Cloudflare Tunnel (check Zero Trust dashboard) |
| CORS errors in browser | Missing `Access-Control-Allow-Origin` header | Add `headers` to your `enclii.yaml` with the allowed origin |
| `502 Bad Gateway` | Pod not ready or wrong port | Verify `runtime.port` matches your server's listen port and health check passes |
