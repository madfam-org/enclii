#!/usr/bin/env python3
"""
check-externalsecret-writers.py — CI lint preventing multi-writer Secret wipes.

THE BUG THIS PREVENTS
=====================
`dhanam-secrets` in namespace `dhanam` is written by THREE ExternalSecrets.
Two declared `creationPolicy: Merge`; one declared `Orphan`.

ESO has no "create-if-missing AND merge" policy. Both `Owner` and `Orphan`
reconcile the target Secret's `data` down to *exactly* the keys that one
ExternalSecret produces — they do not merge. Only `Merge` adds keys without
removing the others'.

So on every 15m refresh the 2-key `Orphan` writer reset the whole Secret to 2
keys; `-extended` re-merged its 10 keys; core re-merged its 13 about 2.5
minutes later. Any pod that started inside that window came up with no
`DATABASE_URL` / `DIRECT_DATABASE_URL` and CrashLooped.

Measured on 2026-08-06: a 6-sample / 42-minute probe caught one sample at 10
keys with `DATABASE_URL` and `DIRECT_DATABASE_URL` absent. Throughout the
whole window every ExternalSecret reported `Ready=True` — the health signal
we had could not see the failure.

WHY THIS IS NOT "2+ WRITERS AND ANY NON-MERGE = FAIL"
=====================================================
`enclii-dhanam-staging/dhanam-secrets` has the same *shape* — a 33-key `Owner`
writer plus a 10-key `Merge` writer — and is NOT degrading. Four live samples
showed 33 keys every time, all 10 of the `Merge` writer's keys present, and an
unchanged resourceVersion.

The reason is key coverage: the staging `Owner` writer's 33 keys are a strict
superset of the `Merge` writer's 10, so when it reconciles the Secret down to
its own key set it removes nothing. In prod there was no superset relationship
(2 keys vs 13 and 10), so each refresh genuinely wiped 21 keys.

So the precise rule this implements is:

  FAIL   2+ writers, at least one `Owner`/`Orphan`, and that writer's key set
         does NOT cover the union of every other writer's keys. The report
         names the exact keys wiped on each refresh — that list is the
         finding.

  WARN   2+ writers, at least one `Owner`/`Orphan`, but its key set DOES cover
         all the others. Nothing breaks today. The fix is NOT to flip
         creationPolicy to Merge: that would leave the target with no creator,
         because no `Merge` writer will ever create the Secret. The remedy is
         to remove the redundant writer so the target has exactly one — or to
         accept it knowing that adding a single key to a `Merge` writer that
         the non-Merge writer lacks silently turns this into the FAIL case.

  PASS   One writer, or every writer is `Merge`.

Details that matter:
  - `spec.target.name` is OPTIONAL; ESO falls back to `metadata.name`. Two
    ExternalSecrets can collide without either naming a target.
  - `spec.target.creationPolicy` is OPTIONAL and defaults to `Owner`.
    Omitting it is choosing the unsafe policy.
  - `spec.target.template.data`, when present, determines the resulting
    Secret's keys — those are used instead of `spec.data[].secretKey`.
  - `spec.dataFrom` produces keys that cannot be enumerated from YAML. On a
    multi-writer target with a non-Merge writer, coverage then cannot be
    proven either way, and an unprovable case is reported as FAIL rather than
    quietly passing.
  - `ClusterExternalSecret` is scanned too. Its `namespaceSelector` cannot be
    resolved statically, so it is attached to every namespace that already has
    a writer for the same target name.

Also reported (warning, not failure):
  - A target written by a single `Merge`-only writer. `Merge` never CREATES
    the target Secret, so unless something else creates it the ExternalSecret
    stays stuck. Repo-only analysis cannot see an out-of-band pre-creation, so
    this is a hint, not a gate.

USAGE
=====
    python3 scripts/check-externalsecret-writers.py infra/k8s/
    python3 scripts/check-externalsecret-writers.py infra/k8s/ infra/argocd/

Exit codes:
  0 — no actively-wiping target (warnings may still be printed)
  1 — at least one target is losing keys on every refresh
  2 — could not parse manifests (YAML error, missing dir, etc.)

Dependency-light (PyYAML only) so it can be copied verbatim into any
ecosystem repo's `scripts/`. The staging manifests live in the dhanam repo,
not here — running this there is what classifies that pair.
"""
from __future__ import annotations

import sys
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterator

try:
    import yaml
except ImportError:  # pragma: no cover
    print("error: PyYAML is required. Install with: pip install pyyaml", file=sys.stderr)
    sys.exit(2)


EXTERNAL_SECRET_KINDS = {"ExternalSecret", "ClusterExternalSecret"}

# Per the ExternalSecret CRD, target.creationPolicy defaults to Owner when
# unset. Owner and Orphan both reconcile the target down to this ES's keys.
DEFAULT_CREATION_POLICY = "Owner"
SAFE_MULTI_WRITER_POLICY = "Merge"

# Sentinel namespace for ClusterExternalSecret, whose target namespaces are
# resolved at runtime by a selector we cannot evaluate from YAML alone.
CLUSTER_SCOPED = "*"

MAX_KEYS_LISTED = 12


@dataclass(frozen=True)
class Writer:
    """One ExternalSecret that writes to a target Secret."""

    file: str
    name: str
    namespace: str
    kind: str
    target_secret: str
    creation_policy: str
    keys: frozenset[str] = field(default_factory=frozenset)
    # False when `dataFrom` (or anything else unresolvable) means the real key
    # set is a superset of `keys` and cannot be known from the manifest.
    keys_complete: bool = True

    @property
    def is_merge(self) -> bool:
        return self.creation_policy == SAFE_MULTI_WRITER_POLICY

    def render(self) -> str:
        keys = f"{len(self.keys)} key(s)" + ("" if self.keys_complete else "+ dataFrom")
        return (
            f"{self.kind}/{self.name} (creationPolicy: {self.creation_policy}, "
            f"{keys}) [{self.file}]"
        )


@dataclass
class Finding:
    severity: str  # "fail" | "warn"
    message: str

    def render(self) -> str:
        prefix = "FAIL" if self.severity == "fail" else "WARN"
        return f"[{prefix}] {self.message}"


def iter_yaml_docs(root: Path) -> Iterator[tuple[Path, dict]]:
    """Yield (path, doc) for every YAML doc under root. Skips non-YAML files."""
    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        if path.suffix not in {".yaml", ".yml"}:
            continue
        try:
            with path.open("r", encoding="utf-8") as fh:
                for doc in yaml.safe_load_all(fh):
                    if isinstance(doc, dict):
                        yield path, doc
        except yaml.YAMLError as exc:
            print(f"error: cannot parse {path}: {exc}", file=sys.stderr)
            raise


def extract_keys(spec: dict, target: dict) -> tuple[frozenset[str], bool]:
    """Return (keys this writer puts in the Secret, whether that list is complete).

    `target.template.data` wins when present: with a template, the rendered
    template keys are the Secret's keys, not the `data[].secretKey` names.
    """
    template = target.get("template")
    if isinstance(template, dict) and isinstance(template.get("data"), dict):
        return frozenset(str(k) for k in template["data"]), True

    keys: set[str] = set()
    for entry in spec.get("data") or []:
        if isinstance(entry, dict) and entry.get("secretKey") is not None:
            keys.add(str(entry["secretKey"]))

    # dataFrom (extract/find) pulls an unknown set of keys out of the store.
    complete = not bool(spec.get("dataFrom"))
    return frozenset(keys), complete


def extract_writer(path: Path, doc: dict) -> Writer | None:
    """Turn one ExternalSecret/ClusterExternalSecret doc into a Writer."""
    kind = doc.get("kind")
    if kind not in EXTERNAL_SECRET_KINDS:
        return None

    meta = doc.get("metadata") or {}
    name = meta.get("name", "<unnamed>")
    spec = doc.get("spec") or {}

    if kind == "ClusterExternalSecret":
        # The real ExternalSecret spec is nested; the namespace is chosen at
        # runtime by namespaceSelector/namespaces.
        spec = spec.get("externalSecretSpec") or {}
        namespace = CLUSTER_SCOPED
    else:
        namespace = meta.get("namespace", "default")

    target = spec.get("target")
    if target is None:
        target = {}
    if not isinstance(target, dict):
        return None

    # target.name is optional — ESO falls back to the ExternalSecret's own name.
    target_secret = str(target.get("name") or name)
    creation_policy = str(target.get("creationPolicy") or DEFAULT_CREATION_POLICY)
    keys, keys_complete = extract_keys(spec, target)

    return Writer(
        file=str(path),
        name=str(name),
        namespace=str(namespace),
        kind=str(kind),
        target_secret=target_secret,
        creation_policy=creation_policy,
        keys=keys,
        keys_complete=keys_complete,
    )


def group_writers(writers: list[Writer]) -> dict[tuple[str, str], list[Writer]]:
    """Group writers by (namespace, target Secret).

    ClusterExternalSecrets are attached to every namespace that already has a
    writer for the same target name, plus their own cluster-scoped bucket.
    That is deliberately conservative: it never invents a namespace, but it
    does catch "a ClusterExternalSecret and a namespaced ExternalSecret both
    own secret X".
    """
    groups: dict[tuple[str, str], list[Writer]] = defaultdict(list)
    namespaced = [w for w in writers if w.namespace != CLUSTER_SCOPED]
    cluster_scoped = [w for w in writers if w.namespace == CLUSTER_SCOPED]

    for w in namespaced:
        groups[(w.namespace, w.target_secret)].append(w)

    for cw in cluster_scoped:
        attached = False
        for (ns, secret), members in list(groups.items()):
            if secret == cw.target_secret:
                members.append(cw)
                attached = True
        if not attached:
            groups[(CLUSTER_SCOPED, cw.target_secret)].append(cw)

    return groups


def _fmt_keys(keys: set[str]) -> str:
    ordered = sorted(keys)
    if len(ordered) > MAX_KEYS_LISTED:
        shown = ordered[:MAX_KEYS_LISTED]
        return ", ".join(shown) + f", … (+{len(ordered) - MAX_KEYS_LISTED} more)"
    return ", ".join(ordered)


def check_group(namespace: str, secret: str, writers: list[Writer]) -> list[Finding]:
    findings: list[Finding] = []
    ns_label = "<any namespace>" if namespace == CLUSTER_SCOPED else namespace

    if len(writers) == 1:
        only = writers[0]
        if only.is_merge:
            findings.append(
                Finding(
                    severity="warn",
                    message=(
                        f"Secret {ns_label}/{secret} has exactly one writer and it "
                        f"is creationPolicy: Merge ({only.render()}). Merge never "
                        "CREATES the target Secret — if nothing pre-creates it the "
                        "ExternalSecret never becomes usable. Verify the Secret is "
                        "pre-created, or switch to Owner while it is a sole writer."
                    ),
                )
            )
        return findings

    non_merge = [w for w in writers if not w.is_merge]
    if not non_merge:
        return findings  # every writer merges — the safe configuration

    writer_list = "\n      ".join(w.render() for w in sorted(writers, key=lambda w: w.name))

    for offender in non_merge:
        others = [w for w in writers if w is not offender]
        others_keys: set[str] = set()
        for o in others:
            others_keys |= set(o.keys)
        others_complete = all(o.keys_complete for o in others)

        # Unenumerable key sets: coverage cannot be proven in either
        # direction. An unprovable case must not read as safe.
        if not offender.keys_complete or not others_complete:
            findings.append(
                Finding(
                    severity="fail",
                    message=(
                        f"Secret {ns_label}/{secret} has {len(writers)} writers and "
                        f"{offender.kind}/{offender.name} uses creationPolicy: "
                        f"{offender.creation_policy}, but the key sets cannot be "
                        "enumerated from the manifests (`dataFrom` pulls an unknown "
                        "set of keys). Whether that writer wipes the others on every "
                        "refresh is therefore unverifiable.\n"
                        f"    Writers:\n      {writer_list}\n"
                        "    Fix: give every writer of this Secret an explicit "
                        "`data:` list, or reduce the target to a single writer. See "
                        "docs/infrastructure/EXTERNAL_SECRETS.md."
                    ),
                )
            )
            continue

        wiped = others_keys - set(offender.keys)

        if wiped:
            findings.append(
                Finding(
                    severity="fail",
                    message=(
                        f"Secret {ns_label}/{secret} LOSES {len(wiped)} key(s) on "
                        f"every refresh: {_fmt_keys(wiped)}.\n"
                        f"    {offender.kind}/{offender.name} uses creationPolicy: "
                        f"{offender.creation_policy} with {len(offender.keys)} key(s), "
                        f"and the other writer(s) contribute {len(others_keys)} key(s) "
                        "it does not produce. Owner and Orphan reconcile the target "
                        "Secret down to exactly their own keys — neither merges — so "
                        "the keys above are deleted on each refresh and only return "
                        "when the other writers next reconcile. Pods starting inside "
                        "that window come up with missing env and CrashLoop, while "
                        "every ExternalSecret still reports Ready=True.\n"
                        f"    Writers:\n      {writer_list}\n"
                        "    Fix: set creationPolicy: Merge on EVERY writer of this "
                        "Secret, and pre-create the Secret (no Merge writer will "
                        "create it). See docs/infrastructure/EXTERNAL_SECRETS.md."
                    ),
                )
            )
        else:
            findings.append(
                Finding(
                    severity="warn",
                    message=(
                        f"Secret {ns_label}/{secret} has {len(writers)} writers and "
                        f"{offender.kind}/{offender.name} uses creationPolicy: "
                        f"{offender.creation_policy} — but its {len(offender.keys)} "
                        f"key(s) cover all {len(others_keys)} key(s) the other "
                        "writer(s) contribute, so nothing is wiped today.\n"
                        f"    Writers:\n      {writer_list}\n"
                        "    Do NOT 'fix' this by flipping creationPolicy to Merge: "
                        "no Merge writer will ever CREATE the Secret, so an all-Merge "
                        "set leaves the target with no creator. The remedy is to "
                        "remove the redundant writer so this Secret has exactly one, "
                        "or to accept it knowing the failure mode — adding a single "
                        "key to a Merge writer that the "
                        f"{offender.creation_policy} writer lacks silently turns this "
                        "into the wiping case, with no other warning."
                    ),
                )
            )

    return findings


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print(
            "usage: check-externalsecret-writers.py <root> [<root> ...]",
            file=sys.stderr,
        )
        return 2

    roots = [Path(p) for p in argv[1:]]
    for root in roots:
        if not root.exists():
            print(f"error: root does not exist: {root}", file=sys.stderr)
            return 2

    writers: list[Writer] = []
    try:
        for root in roots:
            for path, doc in iter_yaml_docs(root):
                w = extract_writer(path, doc)
                if w is not None:
                    writers.append(w)
    except yaml.YAMLError:
        return 2

    groups = group_writers(writers)

    all_findings: list[Finding] = []
    for (namespace, secret), members in sorted(groups.items()):
        all_findings.extend(check_group(namespace, secret, members))

    fails = [f for f in all_findings if f.severity == "fail"]
    warns = [f for f in all_findings if f.severity == "warn"]

    for f in all_findings:
        print(f.render())

    multi = {k: v for k, v in groups.items() if len(v) > 1}
    print()
    print(
        f"checked {len(writers)} ExternalSecret doc(s) writing {len(groups)} "
        f"target Secret(s) in {len(roots)} root(s); {len(multi)} target(s) have "
        f"multiple writers; {len(fails)} failure(s), {len(warns)} warning(s)."
    )

    if fails:
        print(
            "FAIL: multi-writer ExternalSecret policy check failed. See messages above.",
            file=sys.stderr,
        )
        return 1
    print("OK: multi-writer ExternalSecret policy check passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
