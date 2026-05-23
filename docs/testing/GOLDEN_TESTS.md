# Golden manifest tests

Ratcheted Kubernetes manifests under `tests/golden/` prevent accidental drift in rendered config.

## Regenerate

```bash
./scripts/check-golden.sh    # verify
./scripts/update-golden.sh   # refresh after intentional manifest changes
```

See also `SYSTEM_CONTEXT.md` for protected manifest tables.

## Agent indexing

`tests/golden/` is excluded from Cursor index (`.cursorignore`). Contributors changing reconciler output must run the scripts above locally.
