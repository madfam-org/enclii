# MADFAM status policy for PhyndCRM domains

Date: 2026-05-15

## Purpose

`status.madfam.io` must report the externally truthful state of PhyndCRM surfaces, not just whether any Phynd-owned hostname returns HTTP 200.

## Current domain model

- `https://phynd.app` is the public Phynd landing and demo surface.
- `https://crm.madfam.io` is the MADFAM-labelled PhyndCRM tenant slice and must resolve to Janua SSO login for unauthenticated users.
- `https://app.phyne.app` is the generic authenticated PhyneCRM app host for non-MADFAM tenants.
- `https://phynd.app/api/health` is the primary PhyndCRM health endpoint.

## Probe rules

- The Phynd landing entry probes `https://phynd.app` and asserts the correct repository link: `github.com/madfam-org/phynd-crm`.
- The MADFAM slice entry probes `https://crm.madfam.io` and asserts the Janua SSO login copy for MADFAM users.
- The generic app entry probes `https://app.phyne.app` and asserts generic Janua SSO login copy.
- The API entry probes `https://phynd.app/api/health`.

## Truthfulness constraint

Do not represent planned or retired Phynd app surfaces as operational by probing `app.phynd.app`, `admin.phynd.app`, or `api.phynd.app`. If `app.phyne.app` DNS/routing is not provisioned yet, the status page should show that outage until Enclii owns and routes it correctly.
