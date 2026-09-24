# enclii whoami

Display information about the currently authenticated user.

## Synopsis

```bash
enclii [--profile NAME] whoami
```

## Description

The `whoami` command shows which identity the active profile is logged in as:
the profile name, then the email, name and user ID from the stored access
token, the issuer and the token's expiry. Use it to confirm which account a
command will act as before running anything privileged.

> **Output goes to stderr.** `whoami`, `login`, and `logout` report through
> cobra's `cmd.Println`, which writes to `OutOrStderr()`, and the CLI never
> calls `SetOut`. So `enclii whoami > /tmp/who` captures an **empty file** and
> reads as "not logged in". Capture with `enclii whoami 2>&1`.

## Examples

```bash
enclii whoami
enclii --profile admin whoami
```

**Output:**

```text
👤 Currently logged in as:

   Profile: admin
   Email: admin@madfam.io
   ID:    <user id>
   Issuer: https://auth.madfam.io
   Expires: 2026-09-24T01:13:34-06:00
```

When the active profile has no login, it says so and names the command to run,
for example `Not logged in on profile "admin". Run 'enclii --profile admin login' to authenticate.`
The command exits `0` in both cases.

## See Also

- [`enclii login`](./login.md) - Authenticate, and hold several identities with profiles
- [`enclii logout`](./logout.md) - Clear credentials
