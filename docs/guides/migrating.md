---
title: Migrating to Enclii
description: Platform-specific migration guides for Vercel, Railway, and Heroku
sidebar_position: 0
tags: [migration, guides]
---

# Migrating to Enclii

Pick your current platform for a 10-minute, step-by-step migration path.

## Short guides (start here)

- **[From Vercel →](./migrating-from-vercel.md)** — Next.js, static sites, serverless functions, custom domains
- **[From Railway →](./migrating-from-railway.md)** — services, plugins, volumes, private networking
- **[From Heroku →](./migrating-from-heroku.md)** — Procfile, buildpacks, Review Apps, release phase

## Deep-dive guides

Once you've completed the short guide, these full references cover edge cases, incremental rollouts, and platform-specific gotchas:

- [Full Vercel migration guide](./VERCEL_MIGRATION_GUIDE.md)
- [Full Railway migration guide](./RAILWAY_MIGRATION_GUIDE.md)
- [Full Heroku migration guide](./HEROKU_MIGRATION_GUIDE.md)

## Not on one of these platforms?

Enclii deploys any container that implements `/health`. If you're on AWS ECS, Fly.io, Render, DigitalOcean App Platform, or a hand-rolled Kubernetes setup, the [5-minute quickstart](../quickstart.md) is the shortest path.

Need help with a custom migration? Open an issue at [github.com/madfam-org/enclii/issues](https://github.com/madfam-org/enclii/issues) with the tag `migration-help`.
