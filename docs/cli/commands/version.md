# enclii version

Display CLI version and build information.

## Synopsis

```bash
enclii version [flags]
```

## Description

The `version` command displays the CLI version, build information, and checks for available updates.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--short`, `-s` | bool | `false` | Print version number only |
| `--check-update` | bool | `false` | Check for newer version |
| `--output`, `-o` | string | `text` | Output format: `text`, `json` |

## Examples

### Basic Version
```bash
enclii version
```

**Output:**
```
Enclii CLI

Version:    v0.5.2
Commit:     a1b2c3d4
Built:      2025-01-10T15:30:00Z
Go:         go1.22.0
OS/Arch:    darwin/arm64
```

### Short Version
```bash
enclii version --short
```

**Output:**
```
v0.5.2
```

### Check for Updates
```bash
enclii version --check-update
```

**Output:**
```
Enclii CLI v0.5.2

Update available: v0.6.0

What's new:
  - Improved canary deployment monitoring
  - WebSocket log streaming performance
  - New `enclii local infra` commands

Upgrade with:
  brew upgrade enclii
```

### JSON Output
```bash
enclii version -o json
```

**Output:**
```json
{
  "version": "v0.5.2",
  "commit": "a1b2c3d4e5f6",
  "build_time": "2025-01-10T15:30:00Z",
  "go_version": "go1.22.0",
  "os": "darwin",
  "arch": "arm64"
}
```

## Version Scheme

Enclii follows [Semantic Versioning](https://semver.org/):

- **MAJOR** (v1.x.x): Breaking changes
- **MINOR** (vX.1.x): New features, backwards compatible
- **PATCH** (vX.X.1): Bug fixes, backwards compatible

## See Also

- [Installation Guide](../../getting-started/QUICKSTART.md)
- Changelog: `CHANGELOG.md` (repo root)
