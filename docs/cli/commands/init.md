# enclii init

Initialize a new service configuration.

## Synopsis

```bash
enclii init [name] [flags]
```

## Description

The `init` command writes a starter `service.yaml` in the current directory. The service name (and, for now, the project name) is the `name` argument, or the current directory's name when it is omitted. The command fails if `service.yaml` already exists; there is no overwrite flag.

`init` does not inspect the files in the directory. The `--template` value is written as `spec.build.type`, and a catalog slug also prints a pointer to the matching `madfam-org/<slug>-starter` template repository.

## Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | No | Service and project name. Defaults to the current directory's name. |

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--template`, `-t` | string | `auto` | Framework slug from the catalog, or `auto` |

There are no `--name`, `--port`, `--force`, or `--no-detect` flags.

### Template slugs

`auto`, `angular`, `astro`, `django`, `dockerfile`, `express`, `fastapi`, `fastify`, `flask`, `go-chi`, `go-echo`, `go-fiber`, `go-gin`, `go-stdlib`, `nestjs`, `nextjs`, `nuxtjs`, `phoenix`, `rails`, `react`, `remix`, `rust-actix`, `rust-axum`, `static`, `sveltekit`, `vite`, `vue`.

An unknown slug fails with the list of known templates. Short forms such as `node`, `go`, or `python` are rejected.

## Examples

### Initialize using the directory name

```bash
cd my-app
enclii init
```

### Initialize a Next.js service with an explicit name

```bash
enclii init web --template nextjs
```

**Output:**
```
🚂 Initializing Enclii service 'web'...
✅ Created service.yaml
📦 Starter template: https://github.com/madfam-org/nextjs-starter

Next steps:
  1. Review and customize service.yaml
  2. Run 'enclii deploy' to deploy to development
  3. Run 'enclii deploy --env prod' to deploy to production

💡 Learn more at https://enclii.dev/docs
```

## Generated Configuration

`enclii init web --template nextjs` writes:

```yaml
apiVersion: enclii.dev/v1alpha
kind: Service
metadata:
    name: web
    project: web
spec:
    build:
        type: nextjs
    runtime:
        port: 8080
        replicas: 2
        healthCheck: /health
    env:
        - name: NODE_ENV
          value: production
```

The generated `runtime.port` is `8080` and the env block holds `NODE_ENV=production` regardless of template. Edit both to match your application before deploying.

## See Also

- [Service Specification Reference](../../reference/service-spec.md)
- [`enclii deploy`](./deploy.md) - Deploy the service
- [`enclii services-sync`](./services-sync.md) - Sync service definitions from YAML files
