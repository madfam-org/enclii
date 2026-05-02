# enclii signup

Create a new Enclii account via the self-serve signup wizard.

## Synopsis

```bash
enclii signup [--no-browser]
```

## Description

`enclii signup` starts the self-serve signup flow. The full flow — email verification and GitHub OAuth — runs in your browser at `https://app.enclii.dev/signup`. The CLI prints the URL and (by default) opens your default browser.

After completing signup in the browser, return to your terminal and run `enclii login` to authenticate the CLI with the account you just created.

> Sprint 1 note: a fully headless, device-code-style flow is planned for Sprint 2. Today the CLI defers to the browser wizard.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-browser` | bool | `false` | Print the URL only; do not try to open a browser |

## Examples

### Start signup (open browser automatically)

```bash
enclii signup
```

**Output:**
```
Enclii self-serve signup

  Open this URL in your browser to sign up:

    https://app.enclii.dev/signup

  Opening your default browser now…

After completing signup, run `enclii login` to authenticate the CLI.
```

### Headless / SSH session (no browser)

```bash
enclii signup --no-browser
```

Prints the URL but does not attempt to open a browser. Useful when running the CLI over SSH or in a container.

### Override the signup host (self-hosted Enclii)

```bash
ENCLII_APP_BASE_URL=https://app.example.com enclii signup
```

The env var swaps `app.enclii.dev` for the self-hosted host.

## Notes

- The CLI does not store any credentials during signup. All credential issuance happens during `enclii login`.
- On macOS, the browser is opened with `open`; on Windows with `rundll32 url.dll,FileProtocolHandler`; elsewhere with `xdg-open`. If the launch fails, the CLI prints the URL and you can open it manually.
- For self-hosted deployments, set `ENCLII_APP_BASE_URL` to the public URL of your Enclii web app.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | URL printed (and browser launched, if requested) |

`enclii signup` always exits `0` — even if the browser launch fails, since the URL is printed and the user can open it manually.

## See Also

- [`enclii login`](./login.md) - Authenticate the CLI after signup completes
- [`enclii whoami`](./whoami.md) - Verify the active identity
- [`enclii teams`](./teams.md) - Add the new account to a team
