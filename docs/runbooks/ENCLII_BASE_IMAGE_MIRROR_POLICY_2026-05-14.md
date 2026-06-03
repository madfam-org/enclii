# Enclii base image mirror policy - 2026-05-14

## Status

Active.

## Context

During the Phynd production activation work, Enclii Roundhouse builds reached Docker Hub anonymous pull limits while building Switchyard API. The observed failure was Kaniko failing to pull `golang:1.25-alpine` from Docker Hub with `TOOMANYREQUESTS`.

## Policy

Enclii platform Dockerfiles and Roundhouse-generated function Dockerfiles must not rely on unauthenticated Docker Hub pulls for Docker Official Images. Language runtime builder images must use the latest patched patch release accepted by the vulnerability gate, not a floating minor tag.

Use the public ECR Docker Official Image mirror path:

```text
public.ecr.aws/docker/library/<image>:<tag>
```

Examples:

```text
public.ecr.aws/docker/library/golang:1.25.11-alpine
public.ecr.aws/docker/library/node:22-alpine
public.ecr.aws/docker/library/alpine:3.20
```

## Current remediation

The active Enclii service Dockerfiles now use the public ECR mirror for Docker Official Images used by Switchyard API, Roundhouse, Waybill, Admin Console, Docs Site, Switchyard UI, Status, and Landing.

Roundhouse function Dockerfile templates now use the same mirror for generated Go, Python, Node, and Rust function builders/runners where the source image is a Docker Official Image.

## Remaining hardening

`nginxinc/nginx-unprivileged:alpine` is not a Docker Official Image under `library/*`. Do not rewrite it blindly. If it becomes a build blocker, either add authenticated registry pull support for Roundhouse or move the Landing image to a verified unprivileged Nginx base that Enclii controls.

Longer term, Enclii should provide a first-class base image policy surface that can resolve platform-approved base image aliases through an authenticated MADFAM-controlled registry cache.
