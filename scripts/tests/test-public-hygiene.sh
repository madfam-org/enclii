#!/usr/bin/env bash
# Cases for scripts/public-hygiene-check.sh. Each case is a throwaway git repo
# so the guard sees a real `git ls-files` set, which is what it scans.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS="$(cd "$HERE/.." && pwd)"
GUARD="$SCRIPTS/public-hygiene-check.sh"
# The committed-Secret class is a separate parser the guard shells out to. Each
# throwaway repo gets a copy, or the class would be silently absent from every
# case below and the rule would be untested while the suite still printed ok.
SECRET_CHECKER="$SCRIPTS/check-committed-secret-values.py"
TMPROOT="$(mktemp -d)"
trap 'rm -rf "$TMPROOT"' EXIT

fails=0
run=0

# new_repo <name> -> prints the repo path; caller writes files into it
new_repo() {
  local dir="$TMPROOT/$1"
  mkdir -p "$dir/scripts"
  cp "$GUARD" "$dir/scripts/public-hygiene-check.sh"
  cp "$SECRET_CHECKER" "$dir/scripts/check-committed-secret-values.py"
  git -C "$dir" init -q
  git -C "$dir" config user.email t@example.com
  git -C "$dir" config user.name t
  printf '%s\n' "$dir"
}

# expect <name> <expected-exit> <expected-skipped> [pattern-file]
expect() {
  local name="$1" want="$2" want_skipped="$3" patterns="${4:-/nonexistent/patterns}"
  local dir="$TMPROOT/$name" out code
  git -C "$dir" add -A >/dev/null 2>&1
  out=$(MADFAM_HYGIENE_PATTERNS="$patterns" bash "$GUARD" "$dir" 2>&1)
  code=$?
  run=$((run + 1))
  local skipped=0
  [[ "$out" == *"classes_skipped=1"* ]] && skipped=1
  if [[ "$code" != "$want" || "$skipped" != "$want_skipped" ]]; then
    printf 'FAIL %-28s exit=%s (want %s) classes_skipped=%s (want %s)\n' \
      "$name" "$code" "$want" "$skipped" "$want_skipped"
    printf '%s\n' "$out" | sed 's/^/     | /'
    fails=$((fails + 1))
  else
    printf 'ok   %-28s exit=%s classes_skipped=%s\n' "$name" "$code" "$skipped"
  fi
}

# 1. clean tree, no private pattern file -> pass, but the class is SKIPPED
d=$(new_repo clean); echo '# hello' > "$d/README.md"
expect clean 0 1

# 2. placeholder forms are not findings
d=$(new_repo placeholders)
{ echo 'token: ${NPM_TOKEN}'; echo 'auth: YOUR_TOKEN'; echo 'k: <REDACTED>'; echo 'fmt: %s'; } > "$d/notes.md"
expect placeholders 0 1

# 3. private ranges and documented public resolvers are not node identity
d=$(new_repo private-ip)
{ echo '10.0.0.1'; echo '172.18.0.5'; echo '127.0.0.1'; echo '1.1.1.1'; echo '8.8.8.8'; } > "$d/net.md"
expect private-ip 0 1

# 4. a public IPv4 literal in an ops file IS a finding
d=$(new_repo public-ip); echo 'endpoint: 198.18.7.9' > "$d/infra.yaml"
expect public-ip 1 1

# 5. IPv4-shaped strings outside the ops file set are not scanned for this class
d=$(new_repo ip-in-source); echo 'const p = "198.18.7.9"' > "$d/app.ts"
expect ip-in-source 0 1

# 5b. Cloudflare's published range network addresses (NetworkPolicy CIDRs) are not node identity
d=$(new_repo cloudflare-ranges)
{ echo '    - ipBlock:'; echo '        cidr: 173.245.48.0/20'; echo '        cidr: 104.16.0.0/13'; echo '        cidr: 131.0.72.0/22'; } > "$d/netpol.yaml"
expect cloudflare-ranges 0 1

# 5c. any OTHER address inside a Cloudflare range is still a finding (a resolved host is identity)
d=$(new_repo cloudflare-host); echo 'endpoint: 104.21.2.158' > "$d/infra.yaml"
expect cloudflare-host 1 1

# 6. a hardware SKU is a finding wherever it appears
d=$(new_repo hardware-sku); echo 'ordered an EX44 for the cluster' > "$d/CAPACITY.md"
expect hardware-sku 1 1

# 7. an npm registry auth value with a concrete secret is a finding
d=$(new_repo npm-auth); echo '//npm.example.com/:_auth=YWJjZGVmZ2hpamtsbW5vcA==' > "$d/.npmrc"
expect npm-auth 1 1

# 8. a private pattern file that IS readable checks the class (skipped=0)
printf '# comment\n\\bnode-zz-01\\b\n' > "$TMPROOT/patterns.txt"
d=$(new_repo private-clean); echo 'the control-plane node' > "$d/README.md"
expect private-clean 0 0 "$TMPROOT/patterns.txt"

# 9. ... and fails when a needle matches
d=$(new_repo private-hit); echo 'ssh node-zz-01' > "$d/README.md"
expect private-hit 1 0 "$TMPROOT/patterns.txt"

# 10. an empty tracked file set is UNDETERMINED, not clean. Both copied
# scripts must go: the guard excludes itself from the scan, so anything else
# left behind would keep the tracked set non-empty and mask the case.
d=$(new_repo empty)
rm -f "$d/scripts/public-hygiene-check.sh" "$d/scripts/check-committed-secret-values.py"
expect empty 2 0

# --- committed Secret values -------------------------------------------------
# The 2026-09-06 class: a core/v1 Secret holding real credential material in a
# public repo. The bcrypt fixture below is the shape of the verdaccio finding,
# generated for this test and never a live credential.

# 11. a Secret carrying a bcrypt hash is a finding
d=$(new_repo secret-bcrypt)
cat > "$d/secret.yaml" <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: registry-auth
type: Opaque
stringData:
  htpasswd: |
    someone@example.com:$2y$05$Ttf2yPzQ1xKdWnV8sBoLXeR4mHjA7cGuZi0NqEbFvYdM3wSpJlKrO
YAML
expect secret-bcrypt 1 1

# 12. an ExternalSecret is a reference, not material — it passes
d=$(new_repo secret-external)
cat > "$d/es.yaml" <<'YAML'
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: registry-auth
spec:
  target:
    name: registry-auth
  data:
    - secretKey: htpasswd
      remoteRef:
        key: secret/npm-registry
        property: htpasswd
YAML
expect secret-external 0 1

# 13. a Secret whose values are placeholders is a template, not a leak. Every
# placeholder form below is one this repo's own Secret templates already use.
d=$(new_repo secret-placeholders)
cat > "$d/tpl.yaml" <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: tpl
type: Opaque
stringData:
  password: __CHANGE_ME_LOCAL_ONLY__
  token: "${GITHUB_TOKEN}"
  stripe: "<sk_live_xxx>"
  access-key: "REPLACE_ME"
  endpoint: "https://REPLACE_ME.r2.cloudflarestorage.com"
YAML
expect secret-placeholders 0 1

# 14. a name-only Secret shell (no data/stringData) is the approved end state
d=$(new_repo secret-shell)
cat > "$d/shell.yaml" <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: enclii-secrets
type: Opaque
YAML
expect secret-shell 0 1

# 15. non-secret config values in a Secret are not findings. Without this the
# rule would need an allowlist to keep CI green, and an allowlist is the
# mechanism that lets the next real secret in.
d=$(new_repo secret-config-values)
cat > "$d/cfg.yaml" <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: billing
type: Opaque
stringData:
  BILLING_MX_VAT_RATE: "0.16"
  BILLING_ENABLE_OXXO: "true"
  CURRENCY: "MXN"
  url: ghcr.io
  type: helm
  username: madfam-org
  address: "http://vault.vault.svc.cluster.local:8200"
YAML
expect secret-config-values 0 1

# 16. one bad document among good ones in a multi-doc file still fails
d=$(new_repo secret-multidoc)
cat > "$d/multi.yaml" <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: fine
type: Opaque
stringData:
  password: __CHANGE_ME_LOCAL_ONLY__
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
data:
  greeting: hello
---
apiVersion: v1
kind: Secret
metadata:
  name: leaky
type: Opaque
stringData:
  session-key: "Kj8vQm2XpL9wRtYnE7cZa4bHi1Fu6Qs3Nd0Gk5Mv"
YAML
expect secret-multidoc 1 1

# 17. base64 `data:` is decoded before judging — encoding is not redaction
d=$(new_repo secret-base64)
cat > "$d/b64.yaml" <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: encoded
type: Opaque
data:
  password: azhkOFNqMlBxTHc5dlJ0WG5FN2NZbTRiSGoxRnU2QQ==
YAML
expect secret-base64 1 1

# 18. `kind: Secret` nested under ArgoCD ignoreDifferences is a selector, not a
# Secret document. Matching it would fail every repo that configures ArgoCD.
d=$(new_repo secret-nested-selector)
cat > "$d/app.yaml" <<'YAML'
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: demo
spec:
  ignoreDifferences:
    - kind: Secret
      jsonPointers:
        - /data
        - /stringData
YAML
expect secret-nested-selector 0 1

# 19. a placeholder word ADJACENT to real material must not launder it. The
# htpasswd line above is exactly this shape (`…@example.com:$2y$…`): blanking
# the whole token around the word "example" once erased the hash with it.
d=$(new_repo secret-adjacent-placeholder)
cat > "$d/adj.yaml" <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: adjacent
type: Opaque
stringData:
  htpasswd: "example-user:$2y$05$Ttf2yPzQ1xKdWnV8sBoLXeR4mHjA7cGuZi0NqEbFvYdM3wSpJlKrO"
  note: "REPLACE_ME before applying"
YAML
expect secret-adjacent-placeholder 1 1

printf '\npublic-hygiene tests: %s run, %s failed\n' "$run" "$fails"
exit $(( fails > 0 ? 1 : 0 ))
