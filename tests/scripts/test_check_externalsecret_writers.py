"""
Tests for scripts/check-externalsecret-writers.py.

Run with:
    pytest tests/scripts/test_check_externalsecret_writers.py -v

Three states are pinned here, because the difference between them is the whole
point of the tool:

  FAIL  the 2026-08-06 production configuration — a 2-key `Orphan` writer
        alongside 13-key and 10-key `Merge` writers. No key coverage, so 21
        keys were deleted on every 15m refresh.
  WARN  the `enclii-dhanam-staging` configuration — a 33-key `Owner` writer
        alongside a 10-key `Merge` writer whose keys it fully covers. Verified
        healthy in the cluster (four samples, 33 keys each, resourceVersion
        unchanged). Structurally identical to prod, behaviourally not.
  PASS  one writer, or all writers `Merge`.
"""
from __future__ import annotations

import importlib.util
import subprocess
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-externalsecret-writers.py"


# Load the module under test by path (it has hyphens in its name, so we
# can't `import` it directly).
spec = importlib.util.spec_from_file_location("check_es_writers", SCRIPT)
assert spec is not None and spec.loader is not None
check_es_writers = importlib.util.module_from_spec(spec)
# Register before exec so @dataclass can resolve the module via sys.modules.
sys.modules["check_es_writers"] = check_es_writers
spec.loader.exec_module(check_es_writers)


def write_manifest(dir_: Path, name: str, body: str) -> Path:
    p = dir_ / name
    p.write_text(body)
    return p


def run_script(*roots: Path) -> tuple[int, str, str]:
    """Run the script as a subprocess and return (exit_code, stdout, stderr)."""
    proc = subprocess.run(
        [sys.executable, str(SCRIPT), *[str(r) for r in roots]],
        capture_output=True,
        text=True,
    )
    return proc.returncode, proc.stdout, proc.stderr


# ---------------------------------------------------------------------------
# Fixture builders
# ---------------------------------------------------------------------------


def external_secret(
    name: str,
    namespace: str,
    keys: list[str],
    *,
    policy: str | None = "Merge",
    target: str | None = None,
    store: str = "vault-store",
) -> str:
    lines = [
        "apiVersion: external-secrets.io/v1beta1",
        "kind: ExternalSecret",
        "metadata:",
        f"  name: {name}",
        f"  namespace: {namespace}",
        "spec:",
        "  refreshInterval: 15m",
        "  secretStoreRef:",
        f"    name: {store}",
        "    kind: ClusterSecretStore",
        "  target:",
    ]
    if target is not None:
        lines.append(f"    name: {target}")
    if policy is not None:
        lines.append(f"    creationPolicy: {policy}")
    lines.append("    deletionPolicy: Retain")
    lines.append("  data:")
    for key in keys:
        lines += [
            f"    - secretKey: {key}",
            "      remoteRef:",
            "        key: secret/example",
            f"        property: {key.lower()}",
        ]
    return "\n".join(lines) + "\n"


# --- The production configuration that actually broke ----------------------
# Measured 2026-08-06: core 13 keys, extended 10 keys, service-auth bridge 2.
# The bridge's two keys are also produced by `-extended`, so its key set is a
# strict SUBSET of the others — no coverage at all.

PROD_CORE_KEYS = [
    "DATABASE_URL",
    "DIRECT_DATABASE_URL",
    "REDIS_URL",
    "JWT_SECRET",
    "JWT_REFRESH_SECRET",
    "OIDC_CLIENT_ID",
    "OIDC_CLIENT_SECRET",
    "OIDC_ISSUER",
    "STRIPE_SECRET_KEY",
    "STRIPE_WEBHOOK_SECRET",
    "ENCRYPTION_KEY",
    "POSTHOG_API_KEY",
    "CHECKOUT_ALLOWED_HOSTS",
]
PROD_EXTENDED_KEYS = [
    "COTIZA_WEBHOOK_SECRET",
    "FEDERATION_API_TOKEN",
    "METAMAP_CLIENT_ID",
    "METAMAP_CLIENT_SECRET",
    "METAMAP_WEBHOOK_SECRET",
    "METAMAP_FLOW_ID",
    "SMTP_HOST",
    "SMTP_USER",
    "SMTP_PASSWORD",
    "BANXICO_API_TOKEN",
]
PROD_BRIDGE_KEYS = ["COTIZA_WEBHOOK_SECRET", "FEDERATION_API_TOKEN"]

PROD_CORE_MERGE = external_secret(
    "dhanam-secrets", "dhanam", PROD_CORE_KEYS, target="dhanam-secrets"
)
PROD_EXTENDED_MERGE = external_secret(
    "dhanam-secrets-extended", "dhanam", PROD_EXTENDED_KEYS, target="dhanam-secrets"
)
PROD_BRIDGE_ORPHAN = external_secret(
    "dhanam-ecosystem-service-auth",
    "dhanam",
    PROD_BRIDGE_KEYS,
    policy="Orphan",
    target="dhanam-secrets",
    store="kubernetes-store",
)
# Same writer after the enclii#356 fix.
PROD_BRIDGE_MERGE = external_secret(
    "dhanam-ecosystem-service-auth",
    "dhanam",
    PROD_BRIDGE_KEYS,
    policy="Merge",
    target="dhanam-secrets",
    store="kubernetes-store",
)

# --- The staging configuration, which is NOT degrading ---------------------
# 33-key Owner writer whose keys are a strict superset of the 10-key Merge
# writer's. Verified in-cluster: 33 keys across four samples, no oscillation.

STAGING_MERGE_KEYS = [f"EXTENDED_KEY_{i}" for i in range(10)]
STAGING_OWNER_KEYS = STAGING_MERGE_KEYS + [f"CORE_KEY_{i}" for i in range(23)]

STAGING_OWNER_SUPERSET = external_secret(
    "dhanam-secrets",
    "enclii-dhanam-staging",
    STAGING_OWNER_KEYS,
    policy="Owner",
    target="dhanam-secrets",
)
STAGING_EXTENDED_MERGE = external_secret(
    "dhanam-secrets-extended",
    "enclii-dhanam-staging",
    STAGING_MERGE_KEYS,
    policy="Merge",
    target="dhanam-secrets",
)

UNRELATED_KIND = """\
apiVersion: v1
kind: ConfigMap
metadata:
  name: not-an-external-secret
  namespace: dhanam
data:
  hello: world
"""


# ---------------------------------------------------------------------------
# FAIL — the configuration that actually wiped keys
# ---------------------------------------------------------------------------


def test_catches_exact_production_configuration(tmp_path: Path) -> None:
    """2 Merge + 1 Orphan with no key coverage must FAIL and name the wiped keys."""
    write_manifest(tmp_path, "core.yaml", PROD_CORE_MERGE)
    write_manifest(tmp_path, "extended.yaml", PROD_EXTENDED_MERGE)
    write_manifest(tmp_path, "service-auth.yaml", PROD_BRIDGE_ORPHAN)

    code, out, err = run_script(tmp_path)

    assert code == 1, f"expected failure, got {code}\n{out}\n{err}"
    assert "dhanam/dhanam-secrets" in out
    assert "dhanam-ecosystem-service-auth" in out
    assert "Orphan" in out
    # The finding is the key list. 13 + 10 keys contributed, 2 of which the
    # bridge also produces → 21 wiped.
    assert "LOSES 21 key(s)" in out
    assert "DATABASE_URL" in out
    assert "DIRECT_DATABASE_URL" in out
    # Keys the bridge does produce are not claimed as wiped.
    assert "COTIZA_WEBHOOK_SECRET" not in out.split("Writers:")[0]


def test_wiped_key_list_is_exact(tmp_path: Path) -> None:
    """Only the uncovered keys are reported, computed not guessed."""
    write_manifest(
        tmp_path, "a.yaml", external_secret("a", "ns", ["K1", "K2", "K3"], target="shared")
    )
    write_manifest(
        tmp_path,
        "b.yaml",
        external_secret("b", "ns", ["K1"], policy="Owner", target="shared"),
    )

    code, out, _ = run_script(tmp_path)

    assert code == 1
    assert "LOSES 2 key(s) on every refresh: K2, K3" in out


def test_catches_orphan_writer_disjoint_from_others(tmp_path: Path) -> None:
    write_manifest(
        tmp_path, "a.yaml", external_secret("a", "ns", ["ALPHA"], target="shared")
    )
    write_manifest(
        tmp_path,
        "b.yaml",
        external_secret("b", "ns", ["BETA"], policy="Orphan", target="shared"),
    )

    code, out, _ = run_script(tmp_path)

    assert code == 1
    assert "LOSES 1 key(s) on every refresh: ALPHA" in out


# ---------------------------------------------------------------------------
# WARN — the staging configuration: benign, but fragile
# ---------------------------------------------------------------------------


def test_staging_superset_owner_warns_and_does_not_fail(tmp_path: Path) -> None:
    """A 33-key Owner writer covering a 10-key Merge writer wipes nothing.

    Verified in-cluster on 2026-08-06: four samples, 33 keys each, all ten
    extended keys present, resourceVersion unchanged.
    """
    write_manifest(tmp_path, "core.yaml", STAGING_OWNER_SUPERSET)
    write_manifest(tmp_path, "extended.yaml", STAGING_EXTENDED_MERGE)

    code, out, err = run_script(tmp_path)

    assert code == 0, f"staging is healthy and must not fail CI\n{out}\n{err}"
    assert "WARN" in out
    assert "enclii-dhanam-staging/dhanam-secrets" in out
    assert "cover all" in out
    assert "LOSES" not in out


def test_warn_message_rejects_the_flip_to_merge_instinct(tmp_path: Path) -> None:
    """The obvious 'fix' is wrong and the tool must say so.

    Flipping the Owner writer to Merge would leave the target with no creator,
    because no Merge writer ever creates the Secret.
    """
    write_manifest(tmp_path, "core.yaml", STAGING_OWNER_SUPERSET)
    write_manifest(tmp_path, "extended.yaml", STAGING_EXTENDED_MERGE)

    _, out, _ = run_script(tmp_path)

    assert "Do NOT" in out
    assert "no Merge writer will ever CREATE the Secret" in out
    assert "remove the redundant writer" in out


def test_warn_states_the_one_key_away_failure_mode(tmp_path: Path) -> None:
    """Adding one uncovered key to the Merge writer turns WARN into FAIL."""
    write_manifest(tmp_path, "core.yaml", STAGING_OWNER_SUPERSET)
    write_manifest(tmp_path, "extended.yaml", STAGING_EXTENDED_MERGE)
    _, warn_out, _ = run_script(tmp_path)
    assert "silently turns this" in warn_out

    # Now add a single key the Owner writer does not produce.
    write_manifest(
        tmp_path,
        "extended.yaml",
        external_secret(
            "dhanam-secrets-extended",
            "enclii-dhanam-staging",
            STAGING_MERGE_KEYS + ["NEWLY_ADDED_KEY"],
            policy="Merge",
            target="dhanam-secrets",
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "LOSES 1 key(s) on every refresh: NEWLY_ADDED_KEY" in out


def test_equal_key_sets_warn_rather_than_fail(tmp_path: Path) -> None:
    """Coverage is >=, not >. Identical key sets wipe nothing."""
    write_manifest(
        tmp_path, "a.yaml", external_secret("a", "ns", ["K1", "K2"], target="shared")
    )
    write_manifest(
        tmp_path,
        "b.yaml",
        external_secret("b", "ns", ["K1", "K2"], policy="Owner", target="shared"),
    )

    code, out, _ = run_script(tmp_path)

    assert code == 0
    assert "WARN" in out


# ---------------------------------------------------------------------------
# PASS
# ---------------------------------------------------------------------------


def test_passes_on_all_merge_after_the_fix(tmp_path: Path) -> None:
    """The post-enclii#356 state (all three writers Merge) must PASS silently."""
    write_manifest(tmp_path, "core.yaml", PROD_CORE_MERGE)
    write_manifest(tmp_path, "extended.yaml", PROD_EXTENDED_MERGE)
    write_manifest(tmp_path, "service-auth.yaml", PROD_BRIDGE_MERGE)

    code, out, err = run_script(tmp_path)

    assert code == 0, f"expected pass, got {code}\n{out}\n{err}"
    assert "FAIL" not in out
    assert "WARN" not in out


def test_single_owner_writer_passes(tmp_path: Path) -> None:
    """One Owner writer on its own target is the normal, correct case."""
    write_manifest(tmp_path, "owner.yaml", STAGING_OWNER_SUPERSET)

    code, out, _ = run_script(tmp_path)

    assert code == 0
    assert "FAIL" not in out
    assert "WARN" not in out


def test_single_merge_writer_warns_but_passes(tmp_path: Path) -> None:
    """Merge never creates the Secret — warn, but do not block."""
    write_manifest(tmp_path, "merge.yaml", STAGING_EXTENDED_MERGE)

    code, out, _ = run_script(tmp_path)

    assert code == 0
    assert "WARN" in out
    assert "never CREATES" in out


# ---------------------------------------------------------------------------
# Semantics of the ESO fields
# ---------------------------------------------------------------------------


def test_omitted_creation_policy_defaults_to_owner(tmp_path: Path) -> None:
    """An ExternalSecret with no creationPolicy is an Owner writer."""
    write_manifest(
        tmp_path,
        "no-policy.yaml",
        external_secret("a", "ns", ["K1"], policy=None, target="shared"),
    )
    write_manifest(
        tmp_path, "merge.yaml", external_secret("b", "ns", ["K2"], target="shared")
    )

    code, out, _ = run_script(tmp_path)

    assert code == 1
    assert "Owner" in out
    assert "LOSES 1 key(s) on every refresh: K2" in out


def test_omitted_target_name_falls_back_to_metadata_name(tmp_path: Path) -> None:
    """Two ExternalSecrets can collide without either naming a target."""
    write_manifest(
        tmp_path,
        "a.yaml",
        external_secret("shared-secrets", "demo", ["A"], policy="Owner"),
    )
    write_manifest(
        tmp_path,
        "b.yaml",
        external_secret("shared-extra", "demo", ["B"], target="shared-secrets"),
    )

    code, out, _ = run_script(tmp_path)

    assert code == 1
    assert "demo/shared-secrets" in out


def test_datafrom_makes_coverage_unprovable_and_fails(tmp_path: Path) -> None:
    """`dataFrom` keys cannot be enumerated, so coverage cannot be proven.

    An unprovable case must not read as safe.
    """
    data_from = """\
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: bulk
  namespace: ns
spec:
  secretStoreRef:
    name: vault-store
    kind: ClusterSecretStore
  target:
    name: shared
    creationPolicy: Owner
  dataFrom:
    - find:
        name:
          regexp: ".*"
"""
    write_manifest(tmp_path, "bulk.yaml", data_from)
    write_manifest(
        tmp_path, "merge.yaml", external_secret("b", "ns", ["K1"], target="shared")
    )

    code, out, _ = run_script(tmp_path)

    assert code == 1
    assert "cannot be enumerated" in out


def test_datafrom_on_a_single_writer_is_fine(tmp_path: Path) -> None:
    """The repo's one dataFrom example is a sole writer — must not fail."""
    data_from = """\
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: enclii-doppler-full
  namespace: enclii
spec:
  secretStoreRef:
    name: doppler-store
    kind: ClusterSecretStore
  target:
    name: enclii-doppler-full
    creationPolicy: Owner
  dataFrom:
    - find:
        name:
          regexp: ".*"
"""
    write_manifest(tmp_path, "bulk.yaml", data_from)

    code, out, _ = run_script(tmp_path)

    assert code == 0, out


def test_template_data_determines_the_secret_keys(tmp_path: Path) -> None:
    """With target.template.data, the template's keys are the Secret's keys."""
    templated = """\
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: templated
  namespace: ns
spec:
  secretStoreRef:
    name: vault-store
    kind: ClusterSecretStore
  target:
    name: shared
    creationPolicy: Owner
    template:
      data:
        RENDERED_ONE: "{{ .raw }}"
  data:
    - secretKey: raw
      remoteRef:
        key: secret/x
        property: raw
"""
    write_manifest(tmp_path, "templated.yaml", templated)
    write_manifest(
        tmp_path,
        "merge.yaml",
        external_secret("b", "ns", ["RENDERED_ONE"], target="shared"),
    )

    code, out, _ = run_script(tmp_path)

    # The Owner writer emits RENDERED_ONE (not `raw`), which covers the Merge
    # writer's only key — benign.
    assert code == 0, out
    assert "WARN" in out


def test_multi_doc_file_is_scanned(tmp_path: Path) -> None:
    """Writers declared in one multi-document YAML file are still grouped."""
    body = "\n---\n".join([PROD_CORE_MERGE, PROD_BRIDGE_ORPHAN])
    write_manifest(tmp_path, "combined.yaml", body)

    code, out, _ = run_script(tmp_path)

    assert code == 1
    assert "dhanam/dhanam-secrets" in out


def test_same_target_name_in_different_namespaces_is_not_a_collision(
    tmp_path: Path,
) -> None:
    """dhanam/dhanam-secrets and enclii-dhanam-staging/dhanam-secrets are
    different Secrets and must not be grouped together."""
    write_manifest(
        tmp_path,
        "prod.yaml",
        external_secret(
            "dhanam-secrets", "dhanam", PROD_CORE_KEYS, policy="Owner",
            target="dhanam-secrets",
        ),
    )
    write_manifest(tmp_path, "staging.yaml", STAGING_OWNER_SUPERSET)

    code, out, _ = run_script(tmp_path)

    assert code == 0, out


def test_non_externalsecret_docs_are_ignored(tmp_path: Path) -> None:
    write_manifest(tmp_path, "cm.yaml", UNRELATED_KIND)

    code, out, _ = run_script(tmp_path)

    assert code == 0
    assert "checked 0 ExternalSecret doc(s)" in out


def test_cluster_external_secret_colliding_with_namespaced_writer(
    tmp_path: Path,
) -> None:
    """A ClusterExternalSecret targeting the same Secret name as a namespaced
    writer is a real collision even though its namespaces are selector-based."""
    ces = """\
apiVersion: external-secrets.io/v1beta1
kind: ClusterExternalSecret
metadata:
  name: fleet-secrets
spec:
  namespaceSelector:
    matchLabels:
      tier: app
  externalSecretSpec:
    secretStoreRef:
      name: vault-store
      kind: ClusterSecretStore
    target:
      name: dhanam-secrets
      creationPolicy: Owner
    data:
      - secretKey: SHARED
        remoteRef:
          key: secret/fleet
          property: shared
"""
    write_manifest(tmp_path, "core.yaml", PROD_CORE_MERGE)
    write_manifest(tmp_path, "ces.yaml", ces)

    code, out, _ = run_script(tmp_path)

    assert code == 1
    assert "ClusterExternalSecret/fleet-secrets" in out


# ---------------------------------------------------------------------------
# CLI behaviour
# ---------------------------------------------------------------------------


def test_missing_root_exits_2(tmp_path: Path) -> None:
    code, _, err = run_script(tmp_path / "does-not-exist")
    assert code == 2
    assert "does not exist" in err


def test_no_arguments_exits_2() -> None:
    proc = subprocess.run(
        [sys.executable, str(SCRIPT)], capture_output=True, text=True
    )
    assert proc.returncode == 2
    assert "usage:" in proc.stderr


def test_malformed_yaml_exits_2(tmp_path: Path) -> None:
    write_manifest(tmp_path, "broken.yaml", "kind: ExternalSecret\n  bad: [indent\n")
    code, _, err = run_script(tmp_path)
    assert code == 2
    assert "cannot parse" in err


# ---------------------------------------------------------------------------
# The live repository must stay clean
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("root", ["infra/k8s"])
def test_repository_passes(root: str) -> None:
    """infra/k8s must have no actively-wiping multi-writer target.

    This is the ratchet: once green, it stays green.
    """
    code, out, err = run_script(REPO_ROOT / root)
    assert code == 0, f"{root} has multi-writer ExternalSecret violations:\n{out}\n{err}"


def test_repository_is_actually_scanned() -> None:
    """Guards against the check silently scanning nothing."""
    _, out, _ = run_script(REPO_ROOT / "infra" / "k8s")
    assert "checked 0 ExternalSecret doc(s)" not in out
