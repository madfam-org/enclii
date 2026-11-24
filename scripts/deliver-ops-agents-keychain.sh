#!/usr/bin/env bash
#
# Copy the ops agents' Janua client pairs from Vault secret/ops-agents into
# this Mac's login Keychain. OWNER-RUN recovery path for
# `enclii secrets provision oidc --keychain`: use it when that run filed the
# pair in Vault but could not write the Keychain, or to restore the items on a
# new or wiped machine. It never rotates anything.
#
# Owner ruling 2026-11-22 (internal-devops
# decisions/2026-11-22-ops-agents-ecosystem-mcp-access.md): the pairs land at
# secret/ops-agents and go to the ops agents' macOS Keychain, never through
# chat, a repository or a log. Only the platforms that declare
# local_delivery.macos_keychain in config/ecosystem-oidc-provision.yaml are
# accepted (tests/scripts/test_deliver_ops_agents_keychain.py keeps the list
# below equal to the registry).
#
# It needs the Vault admin token and the authorized SSH bastion (on-node k3s
# kubectl), like scripts/apply-switchyard-vault-policy-remote.sh. The token is
# read from stdin, never from argv:
#
#   bash scripts/deliver-ops-agents-keychain.sh ops-agents-karafiel-cogs ops-agents-dhanam-cogs \
#     < ~/.config/madfam/vault-admin.token
#
# (or run it without a redirect and paste the token at the hidden prompt).
#
# Where each value travels: the vault pod (`vault kv get -field`, the token
# read from stdin INSIDE the pod) -> kubectl exec stdout -> the SSH channel ->
# a pipe into python3 -I -> a pipe into `security -i` (commands on stdin, never
# argv). The terminal shows key names, the client id's first 8 characters and
# lengths, nothing else. Items: service madfam-ops-agents-<platform>-cogs,
# accounts client_id and client_secret (-U updates them in place), each read
# back and compared in memory.
#
# Where the token goes is fixed: SSH_HOST, KCTL, VAULT_NS, VAULT_POD and
# VAULT_ADDR are refused if the environment sets them to anything else, and the
# destination is printed before the token is read.
#
# Test mode (DELIVER_KEYCHAIN_TEST_MODE=1, used only by the stub test) allows a
# fake `security`, a fake kubectl and a non-macOS host, and refuses any SSH
# host outside the reserved .invalid domain, so it can never reach the real
# bastion. VAULT_ADDR stays fixed even there.
#
# Last Updated: 2026-11-22
set -euo pipefail

FIXED_SSH_HOST=ssh.madfam.io
FIXED_KCTL="sudo /usr/local/bin/k3s kubectl"
FIXED_VAULT_NS=vault
FIXED_VAULT_POD=vault-0
FIXED_VAULT_ADDR=http://127.0.0.1:8200
SECURITY_BIN=/usr/bin/security
PYTHON_BIN=/usr/bin/python3

# The platforms that declare local_delivery.macos_keychain in the registry.
LOCAL_DELIVERY_PLATFORMS="ops-agents-dhanam-cogs ops-agents-karafiel-cogs"

refuse() { echo "[FAIL] $*" >&2; exit 2; }

if [ "${DELIVER_KEYCHAIN_TEST_MODE:-}" = 1 ]; then
  SSH_HOST="${SSH_HOST:-}"
  KCTL="${KCTL:-}"
  VAULT_NS="${VAULT_NS:-$FIXED_VAULT_NS}"
  VAULT_POD="${VAULT_POD:-$FIXED_VAULT_POD}"
  case "$SSH_HOST" in
    *.invalid) ;;
    *) refuse "test mode refuses SSH host $SSH_HOST (use a .invalid name)" ;;
  esac
  SECURITY_BIN="${TEST_SECURITY_BIN:?test mode needs TEST_SECURITY_BIN}"
  PYTHON_BIN="${TEST_PYTHON_BIN:-python3}"
else
  for var in SSH_HOST KCTL VAULT_NS VAULT_POD VAULT_ADDR; do
    fixed="FIXED_$var"
    if [ -n "${!var+set}" ] && [ "${!var}" != "${!fixed}" ]; then
      refuse "$var is fixed to ${!fixed} outside test mode; unset it"
    fi
  done
  SSH_HOST=$FIXED_SSH_HOST
  KCTL=$FIXED_KCTL
  VAULT_NS=$FIXED_VAULT_NS
  VAULT_POD=$FIXED_VAULT_POD
  if [ "$(uname -s)" != Darwin ]; then
    echo "[FAIL] the login Keychain exists only on macOS" >&2
    exit 1
  fi
fi
if [ -n "${VAULT_ADDR+set}" ] && [ "$VAULT_ADDR" != "$FIXED_VAULT_ADDR" ]; then
  refuse "VAULT_ADDR must be $FIXED_VAULT_ADDR (the vault pod's own listener); unset it"
fi
VAULT_ADDR=$FIXED_VAULT_ADDR
# Every part of the remote command is a plain token: no quotes, no shell syntax.
[[ "$SSH_HOST" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || refuse "SSH_HOST is not a plain host name"
[[ "$KCTL" =~ ^[A-Za-z0-9/._-]+( [A-Za-z0-9/._-]+)*$ ]] || refuse "KCTL is not plain words"
[[ "$VAULT_NS" =~ ^[a-z0-9][a-z0-9-]*$ ]] || refuse "VAULT_NS is not a plain name"
[[ "$VAULT_POD" =~ ^[a-z0-9][a-z0-9-]*$ ]] || refuse "VAULT_POD is not a plain name"

[ "$#" -ge 1 ] || { echo "usage: deliver-ops-agents-keychain.sh <platform>... (one of: $LOCAL_DELIVERY_PLATFORMS)" >&2; exit 2; }
SHORT_NAMES=""
for platform in "$@"; do
  case " $LOCAL_DELIVERY_PLATFORMS " in
    *" $platform "*) ;;
    *) echo "[FAIL] $platform declares no local_delivery.macos_keychain" >&2; exit 2 ;;
  esac
  short="${platform#ops-agents-}"
  SHORT_NAMES="$SHORT_NAMES ${short%-cogs}"
done

echo "[INFO] the token goes to: ssh $SSH_HOST -> $KCTL exec -n $VAULT_NS $VAULT_POD -> vault $VAULT_ADDR" >&2
VAULT_ADMIN_TOKEN=""
if [ -t 0 ]; then
  printf '>> Paste the Vault admin token (input hidden), then press Enter: ' >&2
  IFS= read -rs VAULT_ADMIN_TOKEN || true
  printf '\n' >&2
else
  IFS= read -r VAULT_ADMIN_TOKEN || true
fi
[ -n "$VAULT_ADMIN_TOKEN" ] || { echo "[FAIL] empty token" >&2; exit 1; }
trap 'VAULT_ADMIN_TOKEN=; unset VAULT_ADMIN_TOKEN' EXIT

# Runs in the vault pod. Line 1 of stdin is the token, every later line a key
# name. It answers `<key>=<value>`, `MISSING <key>` or `AUTH_FAILED`, and
# never echoes an error body.
# shellcheck disable=SC2016 # expanded by the pod's shell, not this one
REMOTE_SH='export VAULT_ADDR="$1"
IFS= read -r VT
export VAULT_TOKEN="$VT"
VT=
if ! vault token lookup >/dev/null 2>&1; then echo AUTH_FAILED; exit 0; fi
while IFS= read -r k; do
  [ -n "$k" ] || continue
  if v="$(vault kv get -field="$k" secret/ops-agents 2>/dev/null)"; then
    echo "$k=$v"
  else
    echo "MISSING $k"
  fi
  v=
done
unset VAULT_TOKEN'

case "$REMOTE_SH" in *"'"*) echo "[FAIL] the pod script must not contain a single quote" >&2; exit 1 ;; esac

# Runs locally. Reads the pod's answers from stdin and writes the Keychain.
WRITER_PY='
import os, re, subprocess, sys
sec = sys.argv[1]
platforms = sys.argv[2:]
token = re.compile(r"[A-Za-z0-9][A-Za-z0-9._~-]{0,1023}")
want = {}
for p in platforms:
    want[p + "_cogs_client_id"] = (p, "client_id")
    want[p + "_cogs_client_secret"] = (p, "client_secret")
got = {}
for raw in sys.stdin:
    line = raw.rstrip("\n")
    if line == "AUTH_FAILED":
        sys.exit("[FAIL] Vault refused the admin token; nothing was read")
    if line.startswith("MISSING "):
        sys.exit("[FAIL] " + line[8:] + " is not at secret/ops-agents; provision the platform first")
    key, sep, val = line.partition("=")
    if not sep or key not in want:
        sys.exit("[FAIL] unexpected line from the bastion (not echoed)")
    if not token.fullmatch(val):
        sys.exit("[FAIL] " + key + " is not a plain URL-safe token (value not echoed)")
    got[key] = val
missing = sorted(set(want) - set(got))
if missing:
    sys.exit("[FAIL] no value for " + ", ".join(missing))
for p in platforms:
    svc = "madfam-ops-agents-" + p + "-cogs"
    for acct in ("client_id", "client_secret"):
        val = got[p + "_cogs_" + acct]
        cmd = "add-generic-password -U -s " + svc + " -a " + acct + " -l " + svc + "-" + acct + " -w " + val + "\n"
        r = subprocess.run([sec, "-i"], input=cmd, text=True, capture_output=True)
        if r.returncode != 0:
            sys.exit("[FAIL] keychain write " + svc + "/" + acct + " exited " + str(r.returncode))
        r = subprocess.run([sec, "find-generic-password", "-s", svc, "-a", acct, "-w"], text=True, capture_output=True)
        if r.returncode != 0 or r.stdout.rstrip("\n") != val:
            sys.exit("[FAIL] keychain read-back " + svc + "/" + acct + " does not match")
    cid = got[p + "_cogs_client_id"]
    print("[OK] %s: client_id %s... (%d chars), client_secret stored (%d chars)"
          % (svc, cid[:8], len(cid), len(got[p + "_cogs_client_secret"])))
'

KEYS=""
for short in $SHORT_NAMES; do
  KEYS="$KEYS${short}_cogs_client_id
${short}_cogs_client_secret
"
done

echo "[INFO] reading $(printf '%s' "$KEYS" | grep -c .) key(s) from secret/ops-agents via $SSH_HOST (pod $VAULT_NS/$VAULT_POD)..." >&2
# shellcheck disable=SC2086,SC2029 # SHORT_NAMES holds reviewed plain tokens; the remote command is built here on purpose
{
  printf '%s\n' "$VAULT_ADMIN_TOKEN"
  printf '%s' "$KEYS"
} | ssh "$SSH_HOST" "$KCTL exec -i -n '$VAULT_NS' '$VAULT_POD' -- sh -c '$REMOTE_SH' sh '$VAULT_ADDR'" \
  | "$PYTHON_BIN" -I -c "$WRITER_PY" "$SECURITY_BIN" $SHORT_NAMES
