"""
Tests for infra/scripts/npm/registry-login.py.

Run with:
    pytest tests/scripts/test_registry_login.py -v

Janua (mint, list, revoke) and the registry (/-/whoami) are faked in memory:
no network, no real session. What matters most is checked on every login:
the key never reaches stdout/stderr, the npmrc and the state file are private,
a stale `_authToken` cannot hide the new key, and a key the registry refuses
is revoked rather than left behind.
"""
from __future__ import annotations

import base64
import datetime as dt
import importlib.util
import json
import stat
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "infra" / "scripts" / "npm" / "registry-login.py"

spec = importlib.util.spec_from_file_location("registry_login", SCRIPT)
rl = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rl)

REGISTRY = "https://npm.example/"
NERFED = "//npm.example/"
JANUA = "https://janua.example"
NOW = dt.datetime(2026, 10, 7, 1, 0, tzinfo=dt.timezone.utc)
HOST = "mac"


def jwt(claims: dict) -> str:
    enc = lambda d: base64.urlsafe_b64encode(json.dumps(d).encode()).decode().rstrip("=")
    return f"{enc({'alg': 'RS256'})}.{enc(claims)}.signature"


# Long-lived so tests that move the clock months ahead still hold a session;
# the expired-session case has its own test.
SESSION = jwt({"iss": JANUA, "exp": NOW.timestamp() + 400 * 86400, "aud": "janua.dev"})


class Services:
    """Janua and the registry, in memory."""

    def __init__(self, registry_accepts: bool = True, registry_up: bool = True):
        self.keys = {}
        self.calls = []
        self.registry_accepts = registry_accepts
        self.registry_up = registry_up
        self.minted = 0

    def add(self, name: str, secret: str = None, active: bool = True) -> str:
        self.minted += 1
        kid = f"key-{self.minted}"
        self.keys[kid] = {"id": kid, "name": name, "key": secret or f"sk_live_{self.minted:064x}",
                          "active": active, "scopes": ["npm:install"], "expires_at": None}
        return kid

    def __call__(self, method, url, headers, body):
        data = json.loads(body) if body else None
        self.calls.append((method, url, data))
        if url == REGISTRY + "-/whoami":
            if not self.registry_up:
                raise rl.Undetermined("npm.example: connection refused")
            secret = base64.b64decode(headers["Authorization"].split(" ", 1)[1]).decode().split(":", 1)[1]
            ok = any(k["key"] == secret and k["active"] for k in self.keys.values())
            return (200, {"username": "janua"}) if ok and self.registry_accepts else (401, {"error": "unauthorized"})
        assert headers["Authorization"] == f"Bearer {SESSION}", "Janua calls carry the session"
        if method == "POST" and url == f"{JANUA}/api/v1/api-keys":
            kid = self.add(data["name"])
            self.keys[kid].update(scopes=data["scopes"], expires_at=data["expires_at"])
            k = self.keys[kid]
            return 201, {"id": kid, "key": k["key"], "key_prefix": k["key"][:12], "name": k["name"],
                         "scopes": k["scopes"], "expires_at": k["expires_at"], "created_at": "2026-10-07T01:00:00"}
        if method == "DELETE":
            kid = url.rsplit("/", 1)[1]
            if kid not in self.keys:
                return 404, {}
            self.keys[kid]["active"] = False
            return 204, {}
        if method == "GET" and url.startswith(f"{JANUA}/api/v1/api-keys?"):
            items = [{k: v for k, v in key.items() if k != "key"} for key in self.keys.values() if key["active"]]
            return 200, {"items": items, "total": len(items), "page": 1, "per_page": 100}
        raise AssertionError(f"unexpected call {method} {url}")

    def mints(self):
        return [c for c in self.calls if c[0] == "POST"]

    def revoked(self):
        return [c[1].rsplit("/", 1)[1] for c in self.calls if c[0] == "DELETE"]


@pytest.fixture
def home(tmp_path, monkeypatch):
    monkeypatch.delenv("JANUA_ACCESS_TOKEN", raising=False)
    monkeypatch.delenv("NPM_MADFAM_TOKEN", raising=False)
    monkeypatch.chdir(tmp_path)
    (tmp_path / ".enclii").mkdir()
    (tmp_path / ".enclii" / "credentials.json").write_text(json.dumps({"access_token": SESSION}))
    return tmp_path


def login(home, services, *args, now=NOW):
    ran = []
    code = rl.run_login(rl.parse(["--registry", REGISTRY, "--janua", JANUA, *args]), services,
                        home, now, lambda *a, **k: ran.append(a), HOST)
    return code


def npmrc(home) -> str:
    return (home / ".npmrc").read_text()


def state(home) -> dict:
    return json.loads((home / ".config" / "madfam" / "npm-registry-key.json").read_text())[REGISTRY]


def test_nerf():
    assert rl.nerf("https://npm.madfam.io/") == "//npm.madfam.io/"
    assert rl.nerf("https://npm.madfam.io") == "//npm.madfam.io/"
    assert rl.nerf("http://localhost:4873/sub") == "//localhost:4873/sub/"


def test_rewrite_drops_every_credential_form_and_keeps_the_rest():
    lines = [
        "@madfam:registry=https://npm.example/",
        f"{NERFED}:_authToken=eyJstale",
        f"{NERFED}:_auth=b2xkOm9sZA==",
        f"{NERFED}:username=someone",
        f"{NERFED}:_password=c2VjcmV0",
        f"{NERFED}:always-auth=true",
        "//registry.npmjs.org/:_authToken=${OTHER_REGISTRY_TOKEN}",
        "save-exact=true",
    ]
    kept, removed = rl.rewrite(lines, NERFED, "bmV3")
    assert kept == [
        "@madfam:registry=https://npm.example/",
        "//registry.npmjs.org/:_authToken=${OTHER_REGISTRY_TOKEN}",
        "save-exact=true",
        f"{NERFED}:_auth=bmV3",
    ]
    assert sorted(removed) == ["_auth", "_authToken", "_password", "always-auth", "username"]


def test_first_login_mints_writes_and_proves_the_key_without_printing_it(home, capsys):
    (home / ".npmrc").write_text(f"@madfam:registry={REGISTRY}\n{NERFED}:_authToken=eyJexpired\n")
    services = Services()

    assert login(home, services) == 0

    (method, url, body), = services.mints()
    assert body["scopes"] == ["npm:install"]
    assert body["name"] == "npm:install · mac · 2026-10-07"
    assert body["expires_at"] == "2027-01-05T01:00:00", "naive UTC, no offset"
    secret = services.keys["key-1"]["key"]
    assert credential_of(home) == secret
    assert "_authToken" not in npmrc(home), "a stale _authToken would hide the new key"
    assert state(home)["id"] == "key-1" and "key" not in state(home)
    out = capsys.readouterr()
    assert secret not in out.out + out.err
    assert "OK as janua" in out.out


def credential_of(home) -> str:
    value = rl.credential(npmrc(home).splitlines(), NERFED, "_auth")
    return base64.b64decode(value).decode().split(":", 1)[1]


def test_npmrc_backup_and_state_are_private(home):
    (home / ".npmrc").write_text("save-exact=true\n")
    assert login(home, Services()) == 0
    files = [home / ".npmrc", home / ".config" / "madfam" / "npm-registry-key.json",
             *home.glob(".npmrc.bak-*")]
    assert len(files) == 3
    for path in files:
        assert stat.S_IMODE(path.stat().st_mode) == 0o600, path


def test_a_working_key_far_from_expiry_mints_nothing(home, capsys):
    services = Services()
    assert login(home, services) == 0
    capsys.readouterr()

    assert login(home, services) == 0
    assert len(services.mints()) == 1
    assert "nothing to do" in capsys.readouterr().out


def test_an_expiring_key_is_replaced_and_the_previous_one_revoked(home):
    services = Services()
    assert login(home, services) == 0
    later = NOW + dt.timedelta(days=80)  # 10 days left, under --renew-days 14

    assert login(home, services, now=later) == 0

    assert len(services.mints()) == 2
    assert services.revoked() == ["key-1"]
    assert credential_of(home) == services.keys["key-2"]["key"]
    assert state(home)["id"] == "key-2"


def test_a_refused_fresh_key_puts_the_npmrc_back_and_is_revoked(home, capsys):
    original = f"@madfam:registry={REGISTRY}\n{NERFED}:_authToken=eyJexpired\n"
    (home / ".npmrc").write_text(original)
    services = Services(registry_accepts=False)

    code = rl.main(["--registry", REGISTRY, "--janua", JANUA], transport=services, home=home,
                   now=NOW, run=lambda *a, **k: None)

    assert code == 1
    assert npmrc(home) == original
    assert services.revoked() == ["key-1"]
    assert not (home / ".config" / "madfam" / "npm-registry-key.json").exists()
    assert "refused" in capsys.readouterr().err


def test_check_mints_nothing(home, capsys):
    services = Services()
    assert login(home, services, "--check") == 1
    assert services.mints() == []
    assert login(home, services) == 0
    capsys.readouterr()
    assert login(home, services, "--check") == 0
    assert "OK as janua" in capsys.readouterr().out
    assert len(services.mints()) == 1


@pytest.mark.parametrize("credentials", [None, {"access_token": jwt({"iss": JANUA, "exp": NOW.timestamp() - 5})}])
def test_no_session_or_an_expired_one_is_refused_before_minting(home, credentials):
    path = home / ".enclii" / "credentials.json"
    if credentials is None:
        path.unlink()
    else:
        path.write_text(json.dumps(credentials))
    services = Services()
    with pytest.raises(rl.Refused):
        login(home, services)
    assert services.mints() == []


def test_a_session_from_another_issuer_is_refused(home, monkeypatch):
    monkeypatch.setenv("JANUA_ACCESS_TOKEN", jwt({"iss": "https://elsewhere.example", "exp": NOW.timestamp() + 3600}))
    with pytest.raises(rl.Refused, match="issued by"):
        login(home, Services())


def test_an_unreachable_registry_is_undetermined_not_refused(home, capsys):
    (home / ".npmrc").write_text(f"{NERFED}:_auth=" + base64.b64encode(b"janua:sk_live_x").decode() + "\n")
    code = rl.main(["--registry", REGISTRY, "--janua", JANUA, "--check"],
                   transport=Services(registry_up=False), home=home, now=NOW, run=lambda *a, **k: None)
    assert code == 2
    assert "undetermined" in capsys.readouterr().err


def test_publish_scope_asks_for_both_scopes(home):
    services = Services()
    assert login(home, services, "--scope", "publish") == 0
    assert services.mints()[0][2]["scopes"] == ["npm:install", "npm:publish"]


def test_prune_revokes_only_this_hosts_leftovers(home):
    services = Services()
    leftover = services.add("npm:install · mac workstation + agents · 2026-10-07")
    other_host = services.add("npm:install · macbook · 2026-10-01")
    unrelated = services.add("CI integration")

    assert login(home, services, "--prune") == 0

    assert services.revoked() == [leftover]
    assert services.keys[other_host]["active"] and services.keys[unrelated]["active"]
    assert services.keys["key-4"]["active"], "the fresh key stays"
