# enclii version

Display the CLI version and build information.

## Synopsis

```bash
enclii version [--json]
```

## Description

The `version` command prints the release tag the binary was built from, the
commit, and the build date. Release builds get these values at link time
(see `.github/workflows/cli-release.yml`); a plain `go build` prints the
defaults `1.0.0-alpha`, `development` and `unknown`.

It does not check for updates. Compare the output with the
[releases page](https://github.com/madfam-org/enclii/releases).

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

## Examples

### Text output

```bash
enclii version
```

**Output:**

```text
enclii version v1.0.0-alpha.10
Commit:     0e248103e47f
Build date: 2026-09-24T06:56:28Z
```

Like `login` and `whoami`, the text form is written to stderr, so capture it
with `enclii version 2>&1`.

### JSON output

```bash
enclii version --json
```

**Output** (on stdout):

```json
{"version":"v1.0.0-alpha.10","commit":"0e248103e47f","build_date":"2026-09-24T06:56:28Z"}
```

## Releases

CLI releases are `v*` tags; pushing one runs `cli-release.yml`, which publishes
archives for Linux, macOS and Windows (amd64 and arm64) with a `checksums.txt`.
Tags containing `alpha`, `beta` or `rc` are marked as pre-releases. To upgrade,
download the archive for your platform, verify it, and replace the binary; see
[Installation](../README.md#installation).

## See Also

- [CLI reference](../README.md)
- [Releases](https://github.com/madfam-org/enclii/releases)
