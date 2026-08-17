# Restore from a tenant export

> **Boundary checkpoint (2026-08-17, platform on-call):** Public-safe runbook —
> standard restore recipe using open formats; no secrets, and secret values are
> explicitly the client's to supply (never in the tarball). Private operational
> detail lives in `internal-devops` (2026-08-17 enclii platform security audit).
> Policy: `docs/PUBLIC_REPO_BOUNDARY.md` (repo-boundary contract).


The companion to [tenant-export.md](../architecture/tenant-export.md): how a
client (or a successor operator) turns an `enclii export` tarball back into a
running system — **on enclii, on another Kubernetes cluster, or on plain
Postgres + object storage.** This is the "migrate away" half of the dignified
exit (ADR-0004 in client engagements): a tarball nobody can confidently restore
is a weak guarantee, so this makes the path explicit.

> [!NOTE]
> Nothing here requires enclii. The tarball is standard formats — Kubernetes
> YAML, custom-format `pg_dump`, an sha256 blob inventory — so a departing
> client owes MADFAM nothing to stand their system back up.

## What's in the tarball

`enclii-export-<slug>-<iso_ts>.tar.gz`:

```
manifests/        # K8s: project, services, deployments, cron_jobs, env-vars
                  #   (secret VALUES are redacted — see secrets/ below)
databases/
  <addon>/pg_dump.sql.gz     # custom-format pg_dump per bound addon
blobs/
  inventory.json             # R2 object keys, sizes, sha256 (NOT contents)
secrets/
  references.json            # secret NAMES and types only — never values
README.md                    # layout + this recipe in brief
MANIFEST.json                # sha256 of every file, for integrity
```

## Preconditions

- `kubectl` context on the target cluster (any conformant Kubernetes), OR a
  target Postgres you control.
- `psql` / `pg_restore` (Postgres 15+ client).
- The secret **values** — these are deliberately NOT in the tarball
  (`secrets/references.json` lists names only). You supply them from your own
  vault, or rotate fresh ones. This is a security property, not a gap.
- The R2/object **contents** — the tarball ships a verifiable inventory, not
  the bytes. Pull them from the source bucket (or your mirror) using the
  inventory; verify each against its sha256.

## 1. Verify the tarball

```bash
tar xzf enclii-export-<slug>-<ts>.tar.gz && cd enclii-export-<slug>-<ts>
# Every file matches its recorded digest before you trust any of it:
python3 - <<'PY'
import json, hashlib, sys
m = json.load(open("MANIFEST.json"))
bad = [f for f, want in m["files"].items()
       if hashlib.sha256(open(f,"rb").read()).hexdigest() != want]
sys.exit("CHECKSUM MISMATCH: " + ", ".join(bad) if bad else print("OK: all files verified"))
PY
```

## 2. Restore the databases

For each `databases/<addon>/pg_dump.sql.gz` — restore into a fresh database on
your target Postgres (managed enclii addon, RDS, self-hosted, anything):

```bash
gunzip -c databases/<addon>/pg_dump.sql.gz > /tmp/<addon>.dump
createdb <addon>                       # or provision via your platform
pg_restore --no-owner --role=<your_app_role> -d <addon> /tmp/<addon>.dump
```

`--no-owner` drops the source role assumptions; `--role` maps ownership to
your target's app role. Re-point your app's `DATABASE_URL` at the new host.

## 3. Restore the application

```bash
# Recreate the workloads. Review env-vars first — secret refs are placeholders.
kubectl apply -f manifests/services/
kubectl apply -f manifests/deployments/
kubectl apply -f manifests/cron_jobs/
```

Then, for each entry in `secrets/references.json`, create the Secret with the
value from YOUR vault (or a freshly rotated one):

```bash
kubectl create secret generic <name> --from-literal=<key>=<your_value> -n <ns>
```

Nothing in the manifests carries a live credential — you are always supplying
current values, which is why a leaked tarball is not a leaked system.

## 4. Restore object storage

```bash
# Using blobs/inventory.json (keys + sha256), copy from the source bucket to
# your target and verify. Example with rclone:
jq -r '.objects[].key' blobs/inventory.json | while read k; do
  rclone copyto "source:<bucket>/$k" "dest:<bucket>/$k"
done
# then verify each object's sha256 against the inventory.
```

## 5. Cut over

Point DNS at the new deployment (your registrar — for MADFAM clients the domain
is already theirs, e.g. `creatumundo.mx` on their own Porkbun account), and
retire the source once you've verified the target end to end.

## If restoring back onto enclii (renew / self-manage path)

The same tarball onboards a project via the normal flow: `enclii onboard` for
the namespace/addon/ArgoCD scaffold, then steps 2–4 above against the enclii-
provisioned Postgres and buckets. The managed-Postgres addon restores a
`pg_dump` like any other target.

## What this guarantees

A client can leave with a **verifiable, self-describing** copy of everything —
their data (open-format dumps + inventory), their app (standard K8s manifests),
and the recipe to stand it up anywhere. The only things they must bring are the
secret values and the object bytes, both by design (a portable export must not
double as a credential leak or a multi-TB transfer). That is a dignified exit.
