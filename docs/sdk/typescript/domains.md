---
title: Domains
description: Custom domains are not covered by the Enclii TypeScript SDK
sidebar_position: 13
tags: [sdk, typescript, domains]
---

# Domains

The TypeScript SDK (`@madfam/enclii-sdk` 0.1.0) has **no domains resource**. There is no `enclii.domains` namespace and no domain methods on `services`, so methods such as `domains.add`, `domains.verify`, or `services.listDomains` do not exist.

To manage custom domains:

- Use the CLI: [`enclii domains`](../../cli/commands/domains.md).
- Or call the HTTP API through the client's low-level [`request()` / `get()` / `post()` methods](./index.md#low-level-requests). The API serves custom-domain routes under `/services/{id}/domains`, for example:

  ```typescript
  const domains = await enclii.get<unknown>(`/services/${serviceId}/domains`);
  ```

  The SDK ships no types for these responses; see the [API reference](../../api-reference/index.md) for their shapes.

## Related documentation

- [TypeScript SDK overview](./index.md)
- [CLI: `enclii domains`](../../cli/commands/domains.md)
