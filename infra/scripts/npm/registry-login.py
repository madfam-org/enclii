#!/usr/bin/env python3
"""Give this machine a working npm.madfam.io credential, from a Janua session.

In order:
  1. Reads the credential the npmrc already holds for the registry. If the
     registry accepts it and the key this script minted has more than
     --renew-days left, it stops: nothing to do. Safe to run at every login,
     in an agent's bootstrap, or on a schedule.
  2. Otherwise mints a Janua API key for the session's user, scoped to the
     registry only: `npm:install` (read), or `npm:install` + `npm:publish`
     with --scope publish. It expires after --days (90 by default).
  3. Writes it to the npmrc as HTTP Basic `_auth` and removes the registry's
     other credential lines (`_authToken`, `username`, `_password`): npm reads
     `_authToken` first, so a stale one would hide the new key. The registry
     can use a Janua key in no other way; docs/infrastructure/npm-registry.md
     explains why `_authToken` and `npm login` never work for one.
  4. Proves it: /-/whoami must answer with a user. If the registry refuses the
     fresh key, the npmrc is put back as it was and the key is revoked.
  5. Revokes the key this script minted before (--keep-previous to skip), and
     with --prune every other registry key this host left behind.

The key is never printed, logged, or written anywhere but the npmrc (mode
600). What the script remembers -- key id, prefix, scopes, expiry, no secret
-- lives in ~/.config/madfam/npm-registry-key.json.

Session, nothing to type: $JANUA_ACCESS_TOKEN if set, otherwise the enclii
CLI's own Janua session, refreshed first with `enclii whoami`. Sign in with
`enclii login` when neither exists.

  python3 infra/scripts/npm/registry-login.py            # mint if missing or expiring
  python3 infra/scripts/npm/registry-login.py --check    # verify only, mint nothing
  python3 infra/scripts/npm/registry-login.py --scope publish --days 30

Repositories whose .npmrc reads `_authToken=${NPM_MADFAM_TOKEN}` need that
variable EMPTY, not unset, on a workstation: npm sends an unset `${VAR}` as
the literal text, which hides the Janua key (npm 10 has no `${VAR?}` form).
`export NPM_MADFAM_TOKEN=` does it; CI sets the variable and is unaffected.

Exit status: 0 done; 1 failed (refused, no session, cannot write);
2 undetermined (Janua or the registry could not be reached) -- treat 2 like 1.
"""
from __future__ import annotations

import argparse
import base64
import datetime as dt
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Callable, Dict, List, Optional, Tuple

DEFAULT_REGISTRY = "https://npm.madfam.io/"
DEFAULT_JANUA = "https://auth.madfam.io"
# Cloudflare refuses urllib's default User-Agent (error 1010); say who we are.
USER_AGENT = "madfam-registry-login/1 (+https://npm.madfam.io)"
BASIC_USER = "janua"  # any name works: the plugin reads only the password
SCOPES = {"install": ["npm:install"], "publish": ["npm:install", "npm:publish"]}
CREDENTIAL_KEYS = ("_auth", "_authToken", "username", "_password", "always-auth")

Response = Tuple[int, dict]
Transport = Callable[[str, str, Dict[str, str], Optional[bytes]], Response]


class Undetermined(Exception):
    """A service could not be reached: neither a yes nor a no."""


class Refused(Exception):
    """A service answered, and the answer was no."""


def urllib_transport(method: str, url: str, headers: Dict[str, str], body: Optional[bytes]) -> Response:
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            raw = resp.read()
            return resp.status, (json.loads(raw) if raw else {})
    except urllib.error.HTTPError as err:
        raw = err.read()
        try:
            return err.code, (json.loads(raw) if raw else {})
        except ValueError:
            return err.code, {"detail": raw[:200].decode(errors="replace")}
    except (urllib.error.URLError, socket.timeout, ConnectionError) as err:
        raise Undetermined(f"{urllib.parse.urlsplit(url).netloc}: {err}") from err


def nerf(registry: str) -> str:
    """npm's per-registry config prefix: //host[:port]/path/ without the scheme."""
    parts = urllib.parse.urlsplit(registry)
    path = parts.path if parts.path.endswith("/") else parts.path + "/"
    return f"//{parts.netloc}{path}"


def credential(lines: List[str], nerfed: str, key: str) -> Optional[str]:
    prefix = f"{nerfed}:{key}="
    for line in lines:
        if line.strip().startswith(prefix):
            return line.strip()[len(prefix):]
    return None


def rewrite(lines: List[str], nerfed: str, auth_b64: str) -> Tuple[List[str], List[str]]:
    """Drop every credential line for the registry, then add one `_auth` line."""
    owned = {f"{nerfed}:{k}" for k in CREDENTIAL_KEYS}
    kept, removed = [], []
    for line in lines:
        key = line.strip().split("=", 1)[0].strip()
        if key in owned:
            removed.append(key[len(nerfed) + 1:])
        else:
            kept.append(line)
    kept.append(f"{nerfed}:_auth={auth_b64}")
    return kept, removed


def basic(key: str) -> str:
    return base64.b64encode(f"{BASIC_USER}:{key}".encode()).decode()


def claims(token: str) -> dict:
    """The JWT's claims, unverified: only to say whose and how fresh it is."""
    try:
        part = token.split(".")[1]
        return json.loads(base64.urlsafe_b64decode(part + "=" * (-len(part) % 4)))
    except (IndexError, ValueError):
        return {}


def janua_session(janua: str, home: Path, now: dt.datetime, run=subprocess.run) -> str:
    token = os.environ.get("JANUA_ACCESS_TOKEN")
    source = "JANUA_ACCESS_TOKEN"
    if not token:
        source = "the enclii CLI session"
        if shutil.which("enclii"):
            # Refreshes the session in place when it is close to expiry.
            run(["enclii", "whoami"], capture_output=True, text=True, timeout=60)
        try:
            token = json.loads((home / ".enclii" / "credentials.json").read_text()).get("access_token")
        except (OSError, ValueError):
            token = None
    if not token:
        raise Refused("no Janua session: run `enclii login` (or set JANUA_ACCESS_TOKEN)")
    found = claims(token)
    if str(found.get("iss", "")).rstrip("/") != janua.rstrip("/"):
        raise Refused(f"{source} was issued by {found.get('iss')!r}, not {janua}")
    if float(found.get("exp", 0)) <= now.timestamp() + 60:
        raise Refused(f"{source} has expired: run `enclii login`")
    return token


def whoami(transport: Transport, registry: str, auth_b64: str) -> Optional[str]:
    """The registry's user for this credential; None when it refuses it."""
    status, data = transport(
        "GET",
        urllib.parse.urljoin(registry, "-/whoami"),
        {"Authorization": f"Basic {auth_b64}", "Accept": "application/json", "User-Agent": USER_AGENT},
        None,
    )
    if status == 200 and data.get("username"):
        return str(data["username"])
    if status in (200, 401, 403):
        return None
    raise Undetermined(f"the registry answered {status} to /-/whoami")


def janua_call(transport: Transport, method: str, url: str, token: str, body: Optional[dict] = None) -> Response:
    headers = {"Authorization": f"Bearer {token}", "Accept": "application/json", "User-Agent": USER_AGENT}
    payload = None
    if body is not None:
        headers["Content-Type"] = "application/json"
        payload = json.dumps(body).encode()
    return transport(method, url, headers, payload)


def mint(transport: Transport, janua: str, token: str, scope: str, days: int, host: str, now: dt.datetime) -> Tuple[str, dict]:
    expires = (now.astimezone(dt.timezone.utc) + dt.timedelta(days=days)).replace(microsecond=0, tzinfo=None)
    body = {
        "name": f"npm:{scope} · {host} · {now:%Y-%m-%d}",
        "scopes": SCOPES[scope],
        # Naive UTC, the way api_keys stores every timestamp. An offset made
        # older Janua builds answer 503 instead of creating the key.
        "expires_at": expires.isoformat(),
    }
    status, data = janua_call(transport, "POST", f"{janua.rstrip('/')}/api/v1/api-keys", token, body)
    if status in (200, 201) and data.get("key"):
        key = data.pop("key")
        return key, data
    if status >= 500:
        raise Undetermined(f"Janua answered {status} while minting the key")
    detail = data.get("detail") or data.get("error") or ""
    raise Refused(f"Janua refused to mint the key ({status}): {detail}")


def revoke(transport: Transport, janua: str, token: str, key_id: str) -> bool:
    status, _ = janua_call(transport, "DELETE", f"{janua.rstrip('/')}/api/v1/api-keys/{key_id}", token)
    return status in (200, 204, 404)  # 404: already gone


def host_keys(transport: Transport, janua: str, token: str, host: str) -> List[dict]:
    """Active registry keys this host minted, recognised by their name:
    `npm:<scope> · <host> · …` (the host as a whole word, so `mac` never
    matches `macbook`)."""
    named = re.compile(rf"^npm:\S+ · {re.escape(host)}[ ·]")
    found, page = [], 1
    while True:
        status, data = janua_call(transport, "GET", f"{janua.rstrip('/')}/api/v1/api-keys?page={page}&per_page=100", token)
        if status != 200:
            raise Undetermined(f"Janua answered {status} listing keys")
        items = data.get("items") or []
        found += [k for k in items if named.match(str(k.get("name", "")))]
        if page * 100 >= int(data.get("total", 0)) or not items:
            return found
        page += 1


def load_state(path: Path) -> dict:
    try:
        return json.loads(path.read_text())
    except (OSError, ValueError):
        return {}


def write_private(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as handle:
        handle.write(text)
    os.chmod(path, 0o600)


def days_left(expires_at: Optional[str], now: dt.datetime) -> Optional[float]:
    if not expires_at:
        return None
    when = dt.datetime.fromisoformat(str(expires_at).replace("Z", "+00:00"))
    if when.tzinfo is None:
        when = when.replace(tzinfo=dt.timezone.utc)
    return (when - now).total_seconds() / 86400


def token_hint(cwd: Path) -> Optional[str]:
    rc = cwd / ".npmrc"
    if os.environ.get("NPM_MADFAM_TOKEN") is None and rc.exists() and "${NPM_MADFAM_TOKEN}" in rc.read_text():
        return ("this repository's .npmrc reads ${NPM_MADFAM_TOKEN}: run npm here with the "
                "variable EMPTY (`export NPM_MADFAM_TOKEN=`) so the Janua key is used")
    return None


def parse(argv: Optional[List[str]]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--registry", default=DEFAULT_REGISTRY)
    p.add_argument("--janua", default=DEFAULT_JANUA, help="Janua base URL (the session's issuer)")
    p.add_argument("--scope", choices=sorted(SCOPES), default="install")
    p.add_argument("--days", type=int, default=90, help="lifetime of a newly minted key")
    p.add_argument("--renew-days", type=int, default=14, help="mint a new key when fewer days are left")
    p.add_argument("--npmrc", help="npmrc to write (default: ~/.npmrc)")
    p.add_argument("--check", action="store_true", help="verify the current credential; mint nothing")
    p.add_argument("--force", action="store_true", help="mint a new key even if the current one is fine")
    p.add_argument("--keep-previous", action="store_true", help="do not revoke the key minted before")
    p.add_argument("--prune", action="store_true", help="also revoke every other registry key this host minted")
    args = p.parse_args(argv)
    if args.days < 1 or args.renew_days < 0:
        p.error("--days must be at least 1 and --renew-days at least 0")
    return args


def run_login(args: argparse.Namespace, transport: Transport, home: Path, now: dt.datetime, run, host: str) -> int:
    npmrc = Path(args.npmrc).expanduser() if args.npmrc else home / ".npmrc"
    state_path = home / ".config" / "madfam" / "npm-registry-key.json"
    nerfed = nerf(args.registry)
    state = load_state(state_path)
    mine = state.get(args.registry, {})
    original = npmrc.read_text() if npmrc.exists() else None
    lines = original.splitlines() if original else []

    current = credential(lines, nerfed, "_auth")
    user = whoami(transport, args.registry, current) if current else None
    left = days_left(mine.get("expires_at"), now)
    if args.check:
        if not user:
            print(f"{nerfed} refuses this machine's credential (or there is none): run without --check")
            return 1
        when = f", expires {mine['expires_at']} ({left:.0f} days)" if left is not None else ""
        print(f"{nerfed} OK as {user}{when}")
        return 0
    if user and not args.force and (left is None or left > args.renew_days):
        print(f"{nerfed} OK as {user}; nothing to do")
        return 0

    token = janua_session(args.janua, home, now, run=run)
    key, meta = mint(transport, args.janua, token, args.scope, args.days, host, now)
    new_lines, removed = rewrite(lines, nerfed, basic(key))
    if original is not None:
        write_private(npmrc.with_name(f"{npmrc.name}.bak-{now:%Y%m%dT%H%M%SZ}"), original)
    write_private(npmrc, "\n".join(new_lines) + "\n")
    try:
        user = whoami(transport, args.registry, basic(key))
    except Undetermined:
        state[args.registry] = {**{k: meta.get(k) for k in ("id", "name", "key_prefix", "scopes", "expires_at", "created_at")}, "host": host}
        write_private(state_path, json.dumps(state, indent=2) + "\n")
        raise
    if not user:
        # Put the npmrc back and leave nothing usable behind.
        if original is None:
            npmrc.unlink()
        else:
            write_private(npmrc, original)
        revoke(transport, args.janua, token, str(meta.get("id")))
        raise Refused("the registry refused a freshly minted key; the npmrc is unchanged and the key is revoked")

    state[args.registry] = {**{k: meta.get(k) for k in ("id", "name", "key_prefix", "scopes", "expires_at", "created_at")}, "host": host}
    write_private(state_path, json.dumps(state, indent=2) + "\n")
    revoked = []
    previous = mine.get("id")
    if previous and previous != meta.get("id") and not args.keep_previous and revoke(transport, args.janua, token, str(previous)):
        revoked.append(str(previous))
    if args.prune:
        for k in host_keys(transport, args.janua, token, host):
            if k.get("id") not in (meta.get("id"), *revoked) and revoke(transport, args.janua, token, str(k["id"])):
                revoked.append(str(k["id"]))

    print(f"{nerfed} OK as {user}: key {meta.get('key_prefix')}… ({', '.join(meta.get('scopes') or [])}), "
          f"expires {meta.get('expires_at')}")
    if removed:
        print(f"  removed from {npmrc}: {', '.join(sorted(set(removed)))}")
    if revoked:
        print(f"  revoked: {', '.join(revoked)}")
    hint = token_hint(Path.cwd())
    if hint:
        print(f"  note: {hint}")
    return 0


def main(argv: Optional[List[str]] = None, transport: Transport = urllib_transport,
         home: Optional[Path] = None, now: Optional[dt.datetime] = None, run=subprocess.run) -> int:
    args = parse(argv)
    try:
        return run_login(args, transport, Path(home or Path.home()), now or dt.datetime.now(dt.timezone.utc),
                         run, socket.gethostname().split(".")[0])
    except Refused as err:
        print(f"refused: {err}", file=sys.stderr)
        return 1
    except Undetermined as err:
        print(f"undetermined: {err}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
