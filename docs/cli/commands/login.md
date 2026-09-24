# enclii login

Authenticate with Enclii using SSO (Single Sign-On).

## Synopsis

```bash
enclii [--profile NAME] login [flags]
```

## Description

The `login` command authenticates with Enclii through Janua SSO
(`auth.madfam.io`) using an OAuth 2.0 PKCE flow. By default it opens your
browser. The credentials it receives are stored under the active **profile**
(see [Several identities](#several-identities-profiles-and-account-switching)).

For CI/CD, skip `login` and set `ENCLII_API_TOKEN` (or pass `--api-token`) with
a personal API token.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--prompt` | string | | `select_account` shows Janua's chooser over the accounts the browser holds; `login` forces a fresh sign-in ("sign in as someone else") |
| `--no-browser` | bool | `false` | Print the login URL instead of opening a browser |
| `--issuer` | string | `https://auth.madfam.io` | OAuth issuer URL (or `ENCLII_OIDC_ISSUER`) |
| `--client-id` | string | the Enclii CLI client | OAuth client ID |

Global flags such as `--profile` apply too; see the [CLI reference](../README.md#global-flags).

## Examples

### Interactive login

```bash
enclii login
```

The browser's current Janua session answers silently, so this logs the CLI in as
whichever account that browser is signed in as.

### Choose the account

```bash
enclii login --prompt select_account   # pick from the accounts this browser holds
enclii login --prompt login            # sign in as someone else
```

Choosing an account in Janua's chooser also makes it the browser's active
account, the same as switching accounts in any MADFAM web app.

### Headless or remote machine

```bash
enclii login --no-browser
```

The URL it prints must be opened on the same machine: Janua redirects to the
CLI's local callback on `127.0.0.1`.

## Several identities: profiles and account switching

An operator often needs two Janua identities at once, for example an everyday
account for a client's platform and `admin@madfam.io` for platform
administration. Each **profile** keeps its own login, so switching the CLI's
identity never touches the other login or the browser.

```bash
# One-time: log the "admin" profile in as admin@madfam.io in a private window,
# so the browser's active account stays as it is.
enclii --profile admin login --no-browser --prompt login
#   ...open the printed URL in a private/incognito window and sign in...

enclii --profile admin whoami   # admin@madfam.io
enclii whoami                   # your everyday account, unchanged

# Act as admin only where you need it:
enclii --profile admin secrets provision oidc --platform nauta --reason "..."
ENCLII_PROFILE=admin enclii ops apps status --json
```

To open login URLs in a private window automatically, set `ENCLII_BROWSER` to
a command; the CLI appends the URL as its last argument:

```bash
export ENCLII_BROWSER='open -na "Google Chrome" --args --incognito'   # macOS, Chrome
export ENCLII_BROWSER='firefox --private-window'                       # Linux, Firefox
```

The reverse also works: keep `admin@madfam.io` as the default profile and give
the everyday account a named one.

## Authentication flow

1. The CLI starts an OAuth 2.0 PKCE flow with a local callback on `127.0.0.1`
   (port 8080, or 3000 if 8080 is busy).
2. The browser opens Janua's authorize page, with `prompt` when `--prompt` is set.
3. You authenticate (email and password, OAuth, or passkey), or Janua answers
   from the browser's session.
4. Janua redirects to the local callback.
5. The CLI exchanges the code for tokens.
6. The tokens are saved to the active profile's credentials file.

## Token storage

| Profile | Credentials file |
|---------|------------------|
| `default` (no `--profile`, no `ENCLII_PROFILE`) | `~/.enclii/credentials.json` |
| any other name, e.g. `admin` | `~/.enclii/profiles/admin/credentials.json` |

Files are written with `0600` permissions. Profile names use lowercase letters,
digits, `-` and `_`.

## Security notes

- Tokens are refreshed automatically before they expire.
- `enclii logout` clears only the active profile's credentials.
- For CI/CD, use short-lived API tokens from the dashboard.
- Never commit tokens to version control.

## See Also

- [`enclii logout`](./logout.md) - Clear credentials
- [`enclii whoami`](./whoami.md) - Verify authentication
- [SSO Integration Guide](../../integrations/sso.md)
