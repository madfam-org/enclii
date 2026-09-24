# Function Runtimes

Enclii supports four runtimes for serverless functions, each optimized for different use cases.

## Runtime Comparison

| Runtime | Cold Start | Best For | Base Image |
|---------|------------|----------|------------|
| **Go** | <500ms | APIs, high-performance compute | `gcr.io/distroless/static` |
| **Rust** | <500ms | Maximum performance, systems | `gcr.io/distroless/cc` |
| **Node.js** | <2s | Web backends, rapid development | `node:20-alpine` |
| **Python** | <3s | Data processing, ML inference | `python:3.12-slim` |

## Go Runtime

### Detection

Enclii detects Go functions by:
- `functions/go.mod` file
- `functions/main.go` file

### Handler Pattern

```go
package main

import (
    "net/http"
)

// Handler is the entry point (configurable)
func Handler(w http.ResponseWriter, r *http.Request) {
    // Your function logic
}

func main() {
    http.HandleFunc("/", Handler)
    http.ListenAndServe(":8080", nil)
}
```

### Default Handler

`main.Handler`

### Build Process

1. Static binary compilation with CGO disabled
2. Multi-stage build for minimal image size
3. Final image: `gcr.io/distroless/static-debian12` (~5MB)

### Performance Tips

- Use `sync.Pool` for frequently allocated objects
- Avoid global initialization with external calls
- Pre-warm connections in init() if needed

### Example: JSON API

```go
package main

import (
    "encoding/json"
    "net/http"
    "time"
)

type Request struct {
    Items []int `json:"items"`
}

type Response struct {
    Sum       int       `json:"sum"`
    Timestamp time.Time `json:"timestamp"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req Request
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    sum := 0
    for _, item := range req.Items {
        sum += item
    }

    resp := Response{
        Sum:       sum,
        Timestamp: time.Now(),
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func main() {
    http.HandleFunc("/", Handler)
    http.ListenAndServe(":8080", nil)
}
```

## Python Runtime

### Detection

Enclii detects Python functions by:
- `functions/requirements.txt` file
- `functions/handler.py` file

### Handler Pattern

```python
def main(event, context):
    """
    Args:
        event: Dict with 'body', 'headers', 'method', 'path'
        context: Runtime context (request_id, function_name, etc.)

    Returns:
        Dict with 'statusCode' and 'body'
    """
    return {
        'statusCode': 200,
        'body': 'Hello!'
    }
```

### Default Handler

`handler.main`

### Build Process

1. Install dependencies from `requirements.txt`
2. Pre-compile Python files to bytecode
3. Final image: `python:3.12-slim` + uvicorn

### Performance Tips

- Minimize dependencies in `requirements.txt`
- Use connection pooling for databases
- Pre-import heavy modules at module level

### Example: Data Processing

```python
# handler.py
import json
from datetime import datetime

def main(event, context):
    try:
        body = event.get('body', {})
        if isinstance(body, str):
            body = json.loads(body)

        numbers = body.get('numbers', [])

        result = {
            'count': len(numbers),
            'sum': sum(numbers),
            'average': sum(numbers) / len(numbers) if numbers else 0,
            'processed_at': datetime.utcnow().isoformat()
        }

        return {
            'statusCode': 200,
            'body': json.dumps(result)
        }
    except Exception as e:
        return {
            'statusCode': 500,
            'body': json.dumps({'error': str(e)})
        }
```

## Node.js Runtime

### Detection

Enclii detects Node.js functions by:
- `functions/package.json` file
- `functions/handler.js` file

### Handler Pattern

```javascript
// handler.js
exports.main = async (event, context) => {
    // event: { body, headers, method, path }
    // context: { requestId, functionName }

    return {
        statusCode: 200,
        body: JSON.stringify({ message: 'Hello!' })
    };
};
```

### Default Handler

`handler.main`

### Build Process

1. Install dependencies with `npm ci`
2. Tree-shaking and bundling
3. Final image: `node:20-alpine` (~50MB)

### Performance Tips

- Use native ES modules
- Avoid synchronous file operations
- Bundle with esbuild for smaller size

### Example: Webhook Handler

```javascript
// handler.js
const crypto = require('crypto');

exports.main = async (event, context) => {
    const signature = event.headers['x-webhook-signature'];
    const secret = process.env.WEBHOOK_SECRET;

    // Verify webhook signature
    const expectedSig = crypto
        .createHmac('sha256', secret)
        .update(event.body)
        .digest('hex');

    if (signature !== `sha256=${expectedSig}`) {
        return {
            statusCode: 401,
            body: JSON.stringify({ error: 'Invalid signature' })
        };
    }

    const payload = JSON.parse(event.body);

    // Process webhook
    console.log('Received webhook:', payload.type);

    return {
        statusCode: 200,
        body: JSON.stringify({ received: true })
    };
};
```

## Rust Runtime

### Detection

Enclii detects Rust functions by:
- `functions/Cargo.toml` file

### Handler Pattern

```rust
use warp::Filter;

#[tokio::main]
async fn main() {
    let routes = warp::any().map(|| "Hello!");

    warp::serve(routes)
        .run(([0, 0, 0, 0], 8080))
        .await;
}
```

### Default Handler

`handler` (binary name)

### Build Process

1. Release build with musl for static linking
2. Strip debug symbols
3. Final image: `gcr.io/distroless/cc-debian12` (~10MB)

### Performance Tips

- Use `tokio` for async runtime
- Pre-allocate buffers for repeated operations
- Use `once_cell` for lazy static initialization

### Example: High-Performance API

```rust
use serde::{Deserialize, Serialize};
use warp::Filter;

#[derive(Deserialize)]
struct FibRequest {
    n: u64,
}

#[derive(Serialize)]
struct FibResponse {
    n: u64,
    result: u64,
    duration_ns: u128,
}

fn fibonacci(n: u64) -> u64 {
    if n <= 1 {
        n
    } else {
        let mut a = 0u64;
        let mut b = 1u64;
        for _ in 2..=n {
            let tmp = a + b;
            a = b;
            b = tmp;
        }
        b
    }
}

#[tokio::main]
async fn main() {
    let fib = warp::post()
        .and(warp::path("fibonacci"))
        .and(warp::body::json())
        .map(|req: FibRequest| {
            let start = std::time::Instant::now();
            let result = fibonacci(req.n);
            let duration = start.elapsed();

            let resp = FibResponse {
                n: req.n,
                result,
                duration_ns: duration.as_nanos(),
            };
            warp::reply::json(&resp)
        });

    let health = warp::get()
        .and(warp::path("health"))
        .map(|| warp::reply::json(&serde_json::json!({"status": "ok"})));

    let routes = fib.or(health);

    warp::serve(routes)
        .run(([0, 0, 0, 0], 8080))
        .await;
}
```

## Custom Runtime Configuration

Override the default handler in your function config via the API (the CLI has no `--handler` flag):

```json
{
  "name": "my-function",
  "config": {
    "runtime": "python",
    "handler": "mymodule.process"
  }
}
```

## Environment Variables

All runtimes support environment variables:

Set them through the API's `config.env_vars` (see [Function configuration](./configuration.md#environment-variables)); `enclii functions deploy` has no `--env` flag.

Access in code:

```go
// Go
os.Getenv("DATABASE_URL")
```

```python
# Python
import os
os.environ.get('DATABASE_URL')
```

```javascript
// Node.js
process.env.DATABASE_URL
```

```rust
// Rust
std::env::var("DATABASE_URL")
```
