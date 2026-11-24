"""
Stub test for scripts/deliver-ops-agents-keychain.sh, the owner-run recovery
path of `enclii secrets provision oidc --keychain`.

Run with:
    pytest tests/scripts/test_deliver_ops_agents_keychain.py -v

ssh, kubectl, vault and macOS `security` are fakes in a temporary directory:
no network, no cluster, no real Keychain. The script runs in its test mode,
which refuses any SSH host outside the reserved .invalid domain. Every value
below is an obvious placeholder. What matters most is checked on every run:
no value or token reaches stdout, stderr or any process's argv, and the
script's platform list is exactly the registry's local_delivery set.
"""
from __future__ import annotations

import os
import re
import stat
import subprocess
import sys
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "deliver-ops-agents-keychain.sh"
REGISTRY = REPO_ROOT / "config" / "ecosystem-oidc-provision.yaml"

TOKEN = "fixture-admin-token-0000"  # pragma: allowlist secret
VALUES = {
    "karafiel_cogs_client_id": "jnc_fixtureKarafielClient01",
    "karafiel_cogs_client_secret": "jns_" + "fixture-karafiel-secret-01",  # pragma: allowlist secret
    "dhanam_cogs_client_id": "jnc_fixtureDhanamClient02",
    "dhanam_cogs_client_secret": "jns_" + "fixture-dhanam-secret-02",  # pragma: allowlist secret
}

FAKE_SSH = """#!/bin/sh
printf '%s\\n' "$@" >> "$FAKE_DIR/ssh-argv.log"
exec bash -c "$2"
"""

# Outside test mode the script would run the real `sudo .../k3s kubectl`; this
# stand-in makes sure no test, even a broken one, ever runs sudo for real.
FAKE_SUDO = """#!/bin/sh
printf '%s\\n' "$*" >> "$FAKE_DIR/sudo-argv.log"
echo "fake sudo: refused" >&2
exit 1
"""

FAKE_KUBECTL = """#!/bin/sh
printf '%s\\n' "$@" >> "$FAKE_DIR/kubectl-argv.log"
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
shift
exec "$@"
"""

FAKE_VAULT = """#!/bin/sh
printf '%s\\n' "$*" >> "$FAKE_DIR/vault-argv.log"
[ "$VAULT_TOKEN" = "{token}" ] || exit 2
[ "$1 $2" = "token lookup" ] && exit 0
k="${{3#-field=}}"
v=$(grep "^$k=" "$FAKE_DIR/vault-data" | cut -d= -f2-)
[ -n "$v" ] || exit 2
printf '%s' "$v"
""".format(token=TOKEN)

FAKE_SECURITY = """#!/bin/sh
printf '%s\\n' "$*" >> "$FAKE_DIR/security-argv.log"
store="$FAKE_DIR/store"
if [ "$1" = "-i" ]; then
  while IFS= read -r line; do
    set -- $line
    shift; s=; a=; w=
    while [ "$#" -gt 0 ]; do
      case "$1" in -s) s=$2; shift 2;; -a) a=$2; shift 2;; -w) w=$2; shift 2;; -l) shift 2;; -U) shift;; *) exit 1;; esac
    done
    touch "$store"; grep -v "^$s|$a|" "$store" > "$store.tmp" || true; mv "$store.tmp" "$store"
    printf '%s|%s|%s\\n' "$s" "$a" "$w" >> "$store"
  done
  exit 0
fi
if [ "$1" = find-generic-password ]; then
  shift; s=; a=
  while [ "$#" -gt 0 ]; do case "$1" in -s) s=$2; shift 2;; -a) a=$2; shift 2;; *) shift;; esac; done
  [ -f "$FAKE_DIR/tamper" ] && { echo "jns_something-else"; exit 0; }
  v=$(grep "^$s|$a|" "$store" 2>/dev/null | cut -d'|' -f3)
  [ -n "$v" ] || exit 44
  echo "$v"
  exit 0
fi
exit 1
"""


@pytest.fixture()
def fakes(tmp_path: Path) -> Path:
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    for name, body in {
        "ssh": FAKE_SSH,
        "sudo": FAKE_SUDO,
        "fake-kubectl": FAKE_KUBECTL,
        "vault": FAKE_VAULT,
        "security": FAKE_SECURITY,
    }.items():
        path = bin_dir / name
        path.write_text(body)
        path.chmod(path.stat().st_mode | stat.S_IXUSR)
    (tmp_path / "vault-data").write_text("".join(f"{k}={v}\n" for k, v in VALUES.items()))
    return tmp_path


LOCATION_VARS = ("SSH_HOST", "KCTL", "VAULT_NS", "VAULT_POD", "VAULT_ADDR")


def run(fakes: Path, *platforms: str, token: str = TOKEN, ssh_host: str = "bastion.invalid",
        test_mode: bool = True, extra_env: dict | None = None):
    env = {k: v for k, v in os.environ.items() if k not in LOCATION_VARS}
    env.update(
        PATH=f"{fakes / 'bin'}{os.pathsep}{env.get('PATH', '')}",
        FAKE_DIR=str(fakes),
        TEST_SECURITY_BIN=str(fakes / "bin" / "security"),
        TEST_PYTHON_BIN=sys.executable,
    )
    if test_mode:
        env.update(DELIVER_KEYCHAIN_TEST_MODE="1", SSH_HOST=ssh_host, KCTL=str(fakes / "bin" / "fake-kubectl"))
    else:
        env.pop("DELIVER_KEYCHAIN_TEST_MODE", None)
    env.update(extra_env or {})
    return subprocess.run(
        ["bash", str(SCRIPT), *platforms],
        input=token + "\n",
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
    )


def logs(fakes: Path) -> str:
    return "".join(p.read_text() for p in fakes.glob("*-argv.log"))


def assert_nothing_leaked(fakes: Path, result: subprocess.CompletedProcess) -> None:
    seen = result.stdout + result.stderr + logs(fakes)
    assert TOKEN not in seen
    for value in VALUES.values():
        assert value not in seen


def test_delivers_both_pairs_and_leaks_nothing(fakes: Path) -> None:
    result = run(fakes, "ops-agents-karafiel-cogs", "ops-agents-dhanam-cogs")
    assert result.returncode == 0, result.stderr
    assert result.stdout.splitlines() == [
        "[OK] madfam-ops-agents-karafiel-cogs: client_id jnc_fixt... (27 chars), client_secret stored (30 chars)",
        "[OK] madfam-ops-agents-dhanam-cogs: client_id jnc_fixt... (25 chars), client_secret stored (28 chars)",
    ]
    store = (fakes / "store").read_text().splitlines()
    assert sorted(store) == sorted(
        [
            f"madfam-ops-agents-karafiel-cogs|client_id|{VALUES['karafiel_cogs_client_id']}",
            f"madfam-ops-agents-karafiel-cogs|client_secret|{VALUES['karafiel_cogs_client_secret']}",
            f"madfam-ops-agents-dhanam-cogs|client_id|{VALUES['dhanam_cogs_client_id']}",
            f"madfam-ops-agents-dhanam-cogs|client_secret|{VALUES['dhanam_cogs_client_secret']}",
        ]
    )
    assert_nothing_leaked(fakes, result)


def test_refused_token_reads_nothing(fakes: Path) -> None:
    result = run(fakes, "ops-agents-karafiel-cogs", token="wrong-token")
    assert result.returncode != 0
    assert "Vault refused the admin token" in result.stderr
    assert not (fakes / "store").exists()
    assert_nothing_leaked(fakes, result)


def test_missing_pair_names_the_key_only(fakes: Path) -> None:
    (fakes / "vault-data").write_text(
        "".join(f"{k}={v}\n" for k, v in VALUES.items() if k.startswith("karafiel"))
    )
    result = run(fakes, "ops-agents-dhanam-cogs")
    assert result.returncode != 0
    assert "dhanam_cogs_client_id is not at secret/ops-agents" in result.stderr
    assert not (fakes / "store").exists()
    assert_nothing_leaked(fakes, result)


@pytest.mark.parametrize("platform", ["ops-agents-nauta-cogs", "selva-karafiel-cogs", "karafiel"])
def test_platform_without_local_delivery_is_refused_before_the_token(fakes: Path, platform: str) -> None:
    result = run(fakes, platform)
    assert result.returncode == 2
    assert "declares no local_delivery.macos_keychain" in result.stderr
    assert not (fakes / "ssh-argv.log").exists()


def test_test_mode_never_reaches_a_real_host(fakes: Path) -> None:
    result = run(fakes, "ops-agents-karafiel-cogs", ssh_host="ssh.example.com")
    assert result.returncode == 2
    assert "test mode refuses SSH host" in result.stderr
    assert not (fakes / "ssh-argv.log").exists()


def test_platform_list_matches_the_registry() -> None:
    registry = yaml.safe_load(REGISTRY.read_text())["platforms"]
    declared = {
        pid: p["local_delivery"]["macos_keychain"]
        for pid, p in registry.items()
        if p.get("local_delivery")
    }
    match = re.search(r'^LOCAL_DELIVERY_PLATFORMS="([^"]*)"$', SCRIPT.read_text(), re.M)
    assert match, "LOCAL_DELIVERY_PLATFORMS not found"
    assert sorted(match.group(1).split()) == sorted(declared)
    for pid, service in declared.items():
        assert service == "madfam-" + pid, "the script derives the service name the same way"


@pytest.mark.parametrize(
    "bad",
    ["jns_has space", 'jns_quo"te', "jns_" + "a" * 1021, "-w"],
    ids=["space", "quote", "over-1024", "option"],
)
def test_value_that_is_not_a_plain_token_is_refused_before_security_runs(fakes: Path, bad: str) -> None:
    data = dict(VALUES, karafiel_cogs_client_secret=bad)
    (fakes / "vault-data").write_text("".join(f"{k}={v}\n" for k, v in data.items()))
    result = run(fakes, "ops-agents-karafiel-cogs")
    assert result.returncode != 0
    assert "karafiel_cogs_client_secret is not a plain URL-safe token" in result.stderr
    assert not (fakes / "security-argv.log").exists(), "security ran for a refused value"
    assert bad not in result.stdout + result.stderr


def test_read_back_mismatch_fails(fakes: Path) -> None:
    (fakes / "tamper").write_text("")
    result = run(fakes, "ops-agents-karafiel-cogs")
    assert result.returncode != 0
    assert "keychain read-back madfam-ops-agents-karafiel-cogs/client_id does not match" in result.stderr
    assert "[OK]" not in result.stdout
    assert "jns_something-else" not in result.stdout + result.stderr
    assert_nothing_leaked(fakes, result)


@pytest.mark.parametrize(
    "var,value",
    [
        ("SSH_HOST", "other.invalid"),
        ("KCTL", "kubectl --context elsewhere"),
        ("VAULT_NS", "other"),
        ("VAULT_POD", "vault-1"),
        ("VAULT_ADDR", "http://vault.example:8200"),
    ],
)
def test_outside_test_mode_the_token_destination_is_fixed(fakes: Path, var: str, value: str) -> None:
    result = run(fakes, "ops-agents-karafiel-cogs", test_mode=False, extra_env={var: value})
    assert result.returncode == 2
    assert f"{var} is fixed to" in result.stderr or f"{var} must be" in result.stderr
    assert not (fakes / "ssh-argv.log").exists()
    assert "the token goes to" not in result.stderr, "refused before the token is asked for"


@pytest.mark.parametrize(
    "var,value,message",
    [
        ("VAULT_ADDR", "http://vault.example:8200", "VAULT_ADDR must be http://127.0.0.1:8200"),
        ("KCTL", "kubectl'; true '", "KCTL is not plain words"),
        ("VAULT_POD", "vault 0", "VAULT_POD is not a plain name"),
    ],
)
def test_even_test_mode_validates_the_destination(fakes: Path, var: str, value: str, message: str) -> None:
    result = run(fakes, "ops-agents-karafiel-cogs", extra_env={var: value})
    assert result.returncode == 2
    assert message in result.stderr
    assert not (fakes / "ssh-argv.log").exists()


def test_destination_is_printed_before_the_token_is_read(fakes: Path) -> None:
    result = run(fakes, "ops-agents-karafiel-cogs", token="")
    assert result.returncode != 0
    destination = result.stderr.index(
        f"[INFO] the token goes to: ssh bastion.invalid -> {fakes / 'bin' / 'fake-kubectl'} exec -n vault vault-0 -> vault http://127.0.0.1:8200"
    )
    assert destination < result.stderr.index("[FAIL] empty token")
    assert not (fakes / "ssh-argv.log").exists()
