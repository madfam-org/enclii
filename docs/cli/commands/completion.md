# enclii completion

Generate shell autocompletion scripts.

## Synopsis

```bash
enclii completion <bash|zsh|fish|powershell>
```

## Description

`enclii completion` emits a shell autocompletion script for the `enclii` CLI. The script wires up `<TAB>`-style completion for command names, subcommands, and known flag values.

The command writes the script to **stdout**; you redirect it into your shell's completion path or `source` it on the fly. Restart your shell (or `source` the profile file) for completion to take effect.

## Subcommands

### `bash`

```bash
enclii completion bash > /etc/bash_completion.d/enclii    # system-wide
enclii completion bash > ~/.local/share/bash-completion/completions/enclii  # per-user
# or, on the fly:
source <(enclii completion bash)
```

### `zsh`

```bash
# Per-user, with $fpath/_completions/_enclii:
enclii completion zsh > "${fpath[1]}/_enclii"
# or, on the fly:
source <(enclii completion zsh)
```

If completion is not yet enabled in your zsh, run `echo "autoload -U compinit; compinit" >> ~/.zshrc` once.

### `fish`

```bash
enclii completion fish | source                                     # current shell
enclii completion fish > ~/.config/fish/completions/enclii.fish     # persistent
```

### `powershell`

```powershell
enclii completion powershell | Out-String | Invoke-Expression       # current session
enclii completion powershell >> $PROFILE                            # persistent
```

## Examples

### Install zsh completion (oh-my-zsh)

```bash
mkdir -p ~/.oh-my-zsh/completions
enclii completion zsh > ~/.oh-my-zsh/completions/_enclii
exec zsh   # reload
```

### Install bash completion (Homebrew bash)

```bash
enclii completion bash > $(brew --prefix)/etc/bash_completion.d/enclii
```

### Try completion without installing

```bash
source <(enclii completion bash)
enclii dep<TAB>           # → enclii deploy / enclii deployments
```

## Notes

- The completion script is generated at runtime and reflects whichever subcommands are compiled into your `enclii` binary. Re-generate after upgrading the CLI.
- Completion only suggests command and flag **names** — it does not pre-populate flag values that require API calls (project slugs, service ids, etc.).

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Script written to stdout |

## See Also

- [`enclii version`](./version.md) - Confirm which CLI version you're generating completion for
- [Installation](../README.md#installation) - Install or upgrade the CLI
