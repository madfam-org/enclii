# Refactoring Status & Outstanding Items

This file tracks the status of the April 2026 monorepo reorganization. **Do not delete until all items are cleared.**

## ✅ Completed
- [x] **AI Context**: Unified `.cursorrules` and `CLAUDE.md` into `AI_CONTEXT.md`.
- [x] **Docs**: Consolidated `docs/` hierarchy and archived legacy files.
- [x] **Inlining**: Moved `analytics`, `github`, `sbom`, and `telemetry` micro-packages into core domains.
- [x] **Admin Rename**: Renamed `apps/dispatch` to `apps/admin-console`.

## ⚠️ High Priority Cleanup (Next Session)
- [x] **Flatten Admin Console**: Files are currently at `apps/admin-console/dispatch/`. They must be moved up to `apps/admin-console/` and the nested `dispatch` folder removed.
- [x] **Purge Stale API Dirs**: The `internal/api/` directory has untracked subfolders (e.g., `build/`) from a reverted refactor. Run `rm -rf apps/switchyard-api/internal/api/*/` to clean up.
- [x] **Verify Tests**: Once stale directories are removed, ensure `go test ./internal/api/...` passes without setup errors.

## 🛠 Planned (Phase 2 & 3)
- [x] **API De-monolithing**: Extract shared private helpers from `internal/api/` (e.g., `fetchAndParseEncliiYAML`) into a common package before attempting another sub-package split.
- [x] **UI Component Deduplication**: Move shared primitives from `admin-console` and `switchyard-ui` to `packages/ui-components`.

*Last Updated: 2026-04-30 by Antigravity*
