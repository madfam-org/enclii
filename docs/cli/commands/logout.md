# enclii logout

Clear local authentication credentials.

## Synopsis

```bash
enclii [--profile NAME] logout
```

## Description

The `logout` command deletes the active profile's stored credentials file
(access token, refresh token and expiry). Other profiles keep their logins.

It does not revoke the token server-side, and it does not end your Janua
browser session; sign out in the browser, or revoke tokens from the web
dashboard, for that.

## Examples

```bash
enclii logout                   # the default profile (~/.enclii/credentials.json)
enclii --profile admin logout   # only the "admin" profile
```

## What remains

- Every other profile's login
- API endpoint and other settings (environment variables, `~/.enclii/config.yml`)

## See Also

- [`enclii login`](./login.md) - Authenticate with Enclii
- [`enclii whoami`](./whoami.md) - Check current authentication
