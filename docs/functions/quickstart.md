# Functions Quickstart

Get started with Enclii's serverless functions in under 5 minutes. Functions automatically scale to zero when idle and scale up on demand.

## Prerequisites

- Enclii CLI installed (`enclii --version`)
- An Enclii project created (`enclii init`)
- Authenticated with Enclii (`enclii login`)

## 1. Create a Function

Create a `functions/` directory in your project root:

```bash
mkdir -p functions
```

### Go Function

```go
// functions/main.go
package main

import (
    "encoding/json"
    "net/http"
)

type Request struct {
    Name string `json:"name"`
}

type Response struct {
    Message string `json:"message"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
    var req Request
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        req.Name = "World"
    }

    resp := Response{
        Message: "Hello, " + req.Name + "!",
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func main() {
    http.HandleFunc("/", Handler)
    http.ListenAndServe(":8080", nil)
}
```

Add a `go.mod`:

```bash
cd functions && go mod init hello-function
```

### Python Function

```python
# functions/handler.py
import json

def main(event, context):
    body = event.get('body', {})
    if isinstance(body, str):
        body = json.loads(body)

    name = body.get('name', 'World')

    return {
        'statusCode': 200,
        'body': json.dumps({
            'message': f'Hello, {name}!'
        })
    }
```

Add a `requirements.txt`:

```
# functions/requirements.txt
# Add any dependencies here
```

### Node.js Function

```javascript
// functions/handler.js
exports.main = async (event, context) => {
    const body = typeof event.body === 'string'
        ? JSON.parse(event.body)
        : event.body || {};

    const name = body.name || 'World';

    return {
        statusCode: 200,
        body: JSON.stringify({
            message: `Hello, ${name}!`
        })
    };
};
```

Add a `package.json`:

```json
{
    "name": "hello-function",
    "version": "1.0.0",
    "main": "handler.js"
}
```

### Rust Function

```rust
// functions/src/main.rs
use serde::{Deserialize, Serialize};
use warp::Filter;

#[derive(Deserialize)]
struct Request {
    name: Option<String>,
}

#[derive(Serialize)]
struct Response {
    message: String,
}

#[tokio::main]
async fn main() {
    let handler = warp::post()
        .and(warp::body::json())
        .map(|req: Request| {
            let name = req.name.unwrap_or_else(|| "World".to_string());
            let resp = Response {
                message: format!("Hello, {}!", name),
            };
            warp::reply::json(&resp)
        });

    warp::serve(handler)
        .run(([0, 0, 0, 0], 8080))
        .await;
}
```

## 2. Deploy the Function

Deploy your function to Enclii:

```bash
enclii functions deploy --project my-project
```

The CLI will:
1. Detect the runtime from your project files
2. Build a minimal container image
3. Deploy with KEDA scale-to-zero enabled
4. Return the function endpoint

Expected output:

```
Deploying function 'hello' (go runtime) to project 'my-project'...
Function created: 550e8400-e29b-41d4-a716-446655440000
Status: building
Endpoint: https://hello.fn.enclii.dev (pending deployment)
```

## 3. Invoke the Function

Once deployed, invoke your function:

```bash
# CLI invocation
enclii functions invoke hello --data '{"name":"Developer"}'

# HTTP invocation
curl -X POST https://hello.fn.enclii.dev \
  -H "Content-Type: application/json" \
  -d '{"name":"Developer"}'
```

Response:

```json
{
  "statusCode": 200,
  "body": "{\"message\":\"Hello, Developer!\"}",
  "duration": "15ms",
  "coldStart": true
}
```

## 4. Monitor Your Function

View function status and metrics:

```bash
# List all functions
enclii functions list

# View detailed info
enclii functions info hello

# Stream logs
enclii functions logs hello --follow
```

Or visit the dashboard at `https://app.enclii.dev/functions`.

## 5. Scale-to-Zero in Action

After 5 minutes of inactivity, your function scales to zero replicas:

```bash
$ enclii functions list
NAME     RUNTIME  STATUS  INVOCATIONS  AVG MS  LAST INVOKED
hello    go       Ready   15           12ms    5 minutes ago
```

When invoked again, it scales back up automatically (cold start ~500ms for Go/Rust).

## Next Steps

- [Runtime Configuration](./runtimes.md) - Detailed runtime-specific guides
- [Configuration Reference](./configuration.md) - All configuration options
- [KEDA Functions Infrastructure](../infrastructure/KEDA_FUNCTIONS.md) - KEDA and auto-scaling options

## Troubleshooting

### Build Fails

Check if your runtime files are detected:

```bash
ls functions/
# Should contain: go.mod, requirements.txt, package.json, or Cargo.toml
```

### Function Not Ready

Check the function status:

```bash
enclii functions info hello
```

If status is "failed", check logs:

```bash
enclii functions logs hello
```

### Cold Start Too Slow

Consider:
- Using Go or Rust for <500ms cold starts
- Setting `minReplicas: 1` for latency-sensitive functions
- Reducing dependencies for faster initialization
