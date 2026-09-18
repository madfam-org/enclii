#!/usr/bin/env bash
#
# check-platform-infra-copies.sh
#
# Prevent silent drift between an ArgoCD-synced platform-infra manifest and its
# canonical source elsewhere in the tree.
#
# Why this script exists
# ----------------------
# infra/k8s/platform-infra/ is the `manifestPath` of the
# `platform-infra-services` ArgoCD Application
# (infra/argocd/projects/platform-infra/config.json) — it is what actually gets
# applied to the cluster. A few files there are MAINTAINED COPIES of a canonical
# source elsewhere in the tree: infra/k8s/README.md documents `redis.yaml` as a
# "copy of production/data/redis.yaml". They cannot be real symlinks because
# ArgoCD refuses to follow a symlink that escapes the app path, so each copy is
# a plain file that can — and did — drift.
#
# On 2026-09-17 that drift caused a production incident. The canonical
# production/data/redis.yaml carried `--maxmemory 200mb`, `--maxmemory-policy
# allkeys-lru` and a 512Mi limit, but the ArgoCD-synced platform-infra/redis.yaml
# had lost all three. The shared `data/redis` cache therefore ran with no memory
# cap, grew past its 256Mi container limit and was OOMKilled roughly every 37
# minutes — and every Janua hosted magic-link callback that landed in an OOM
# window dead-ended. The fix (enclii#563) existed in the canonical file; it had
# simply never been mirrored into the copy ArgoCD reads. This check turns that
# class of silent drift into a red build instead of a prod incident.
#
# What it verifies
# ----------------
#   For every registered pair, the ArgoCD-synced copy is byte-for-byte identical
#   to its canonical source.
#
# Intentionally NOT registered
# ----------------------------
#   platform-infra/postgres-backup.yaml is described in the README as a "symlink
#   to production/backup/", but it is a DIFFERENT file from
#   production/backup/postgres-backup.yaml (a wrapper, not a byte copy). That is
#   a different relationship, so it is excluded here — do not add it expecting
#   byte-equality.
#
# Adding a pair
# -------------
#   Append "<argocd-synced copy>|<canonical source>" to COPY_PAIRS below.
#
# Performance: <1s, zero dependencies beyond bash + diff (present on every
# GitHub-hosted and self-hosted ARC runner).
#
# Exit codes:
#   0  all registered copies are in sync
#   1  drift detected, or a registered file is missing

set -euo pipefail

# Resolve repo root regardless of where the script is invoked from.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# "<argocd-synced copy>|<canonical source>" — paths relative to repo root.
COPY_PAIRS=(
  "infra/k8s/platform-infra/redis.yaml|infra/k8s/production/data/redis.yaml"
)

fail() { echo "❌ $*" >&2; exit 1; }
pass() { echo "✅ $*"; }

drift=0
for pair in "${COPY_PAIRS[@]}"; do
  synced="${pair%%|*}"
  canonical="${pair##*|}"

  [ -f "$ROOT/$synced" ]    || fail "missing synced copy: $synced"
  [ -f "$ROOT/$canonical" ] || fail "missing canonical source: $canonical"

  # Used as the `if` condition so `set -e` does not abort on diff's exit 1.
  if diff_out="$(diff -u "$ROOT/$canonical" "$ROOT/$synced")"; then
    pass "$synced is in sync with $canonical"
  else
    drift=1
    {
      echo "❌ DRIFT: $synced has diverged from its canonical source $canonical"
      echo ""
      printf '%s\n' "$diff_out" | sed 's/^/   /'
      echo ""
      echo "   $canonical is the canonical source (infra/k8s/README.md), but"
      echo "   $synced is what ArgoCD actually applies — they must be identical."
      echo "   Fix: put the intended change in BOTH in the same commit, e.g."
      echo "     cp \"$canonical\" \"$synced\"   # if the canonical is the correct one"
      echo "   then re-run: ./scripts/check-platform-infra-copies.sh"
    } >&2
  fi
done

[ "$drift" -eq 0 ] || exit 1

echo ""
pass "all platform-infra synced copies match their canonical sources"
