#!/usr/bin/env python3
"""Migrate live ExternalSecret target values into their Vault KV paths.

This script is for Vault rebootstrap recovery. It reads ExternalSecret specs
from the live cluster, reads their current target Kubernetes Secret values, and
writes those values to the exact Vault `remoteRef.key` / `remoteRef.property`
that External Secrets Operator expects.

Secret values are never printed. Dry-run mode reports only names, counts, and
missing key diagnostics.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import subprocess
import sys
from collections import defaultdict
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class Mapping:
    namespace: str
    external_secret: str
    target_secret: str
    secret_key: str
    vault_path: str
    vault_property: str


_POD_ENV_CACHE: dict[tuple[str, str, bool], tuple[bool, str]] = {}
_POD_ENV_KEY_CACHE: dict[str, set[str]] = {}


def run_capture(
    args: list[str],
    *,
    input_text: str | None = None,
    timeout: int = 20,
) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(
            args,
            input=input_text,
            text=True,
            capture_output=True,
            check=False,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired as exc:
        stdout = exc.stdout if isinstance(exc.stdout, str) else ""
        stderr = exc.stderr if isinstance(exc.stderr, str) else "command timed out"
        return subprocess.CompletedProcess(args, 124, stdout, stderr)


def kubectl_json(args: list[str]) -> dict[str, Any]:
    result = run_capture(["kubectl", *args, "-o", "json"])
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or result.stdout.strip())
    return json.loads(result.stdout)


def decode_secret_value(raw: str) -> str:
    return base64.b64decode(raw).decode("utf-8", errors="replace")


def load_mappings(store_name: str) -> tuple[list[Mapping], list[str]]:
    data = kubectl_json(["get", "externalsecrets.external-secrets.io", "-A"])
    mappings: list[Mapping] = []
    skipped: list[str] = []

    for item in data.get("items", []):
        meta = item.get("metadata", {})
        spec = item.get("spec", {})
        namespace = meta.get("namespace", "")
        es_name = meta.get("name", "")
        store = spec.get("secretStoreRef", {})
        if store.get("name") != store_name:
            continue

        target_secret = spec.get("target", {}).get("name") or es_name
        if spec.get("dataFrom"):
            skipped.append(f"{namespace}/{es_name}: dataFrom is not supported")

        for entry in spec.get("data") or []:
            remote = entry.get("remoteRef") or {}
            vault_path = remote.get("key")
            secret_key = entry.get("secretKey")
            if not vault_path or not secret_key:
                skipped.append(f"{namespace}/{es_name}: incomplete data mapping")
                continue
            if not vault_path.startswith("secret/"):
                skipped.append(f"{namespace}/{es_name}: non-Vault path {vault_path}")
                continue
            mappings.append(
                Mapping(
                    namespace=namespace,
                    external_secret=es_name,
                    target_secret=target_secret,
                    secret_key=secret_key,
                    vault_path=vault_path,
                    vault_property=remote.get("property") or secret_key,
                )
            )

    return mappings, skipped


def find_pod_env_value(
    namespace: str,
    key: str,
    *,
    dry_run: bool,
) -> tuple[bool, str]:
    cache_key = (namespace, key, dry_run)
    if cache_key in _POD_ENV_CACHE:
        return _POD_ENV_CACHE[cache_key]

    if dry_run and namespace in _POD_ENV_KEY_CACHE:
        found = key in _POD_ENV_KEY_CACHE[namespace]
        _POD_ENV_CACHE[cache_key] = (found, "")
        return _POD_ENV_CACHE[cache_key]

    pods = kubectl_json(["-n", namespace, "get", "pods"])
    env_keys: set[str] = set()
    for pod in pods.get("items", []):
        status = pod.get("status", {})
        if status.get("phase") != "Running":
            continue
        pod_name = pod.get("metadata", {}).get("name", "")
        for container in pod.get("spec", {}).get("containers", []):
            container_name = container.get("name", "")
            if dry_run:
                result = run_capture(
                    [
                        "kubectl",
                        "-n",
                        namespace,
                        "exec",
                        pod_name,
                        "-c",
                        container_name,
                        "--",
                        "sh",
                        "-c",
                        "env | cut -d= -f1",
                    ],
                    timeout=5,
                )
                if result.returncode == 0:
                    env_keys.update(line for line in result.stdout.splitlines() if line)
                continue

            result = run_capture(
                [
                    "kubectl",
                    "-n",
                    namespace,
                    "exec",
                    pod_name,
                    "-c",
                    container_name,
                    "--",
                    "printenv",
                    key,
                ],
                timeout=10,
            )
            if result.returncode == 0:
                _POD_ENV_CACHE[cache_key] = (True, result.stdout.rstrip("\n"))
                return _POD_ENV_CACHE[cache_key]

    if dry_run:
        _POD_ENV_KEY_CACHE[namespace] = env_keys
        found = key in env_keys
        _POD_ENV_CACHE[cache_key] = (found, "")
        return _POD_ENV_CACHE[cache_key]

    _POD_ENV_CACHE[cache_key] = (False, "")
    return _POD_ENV_CACHE[cache_key]


def load_target_secrets(
    mappings: list[Mapping],
    *,
    fallback_pod_env: bool,
    dry_run: bool,
) -> tuple[dict[str, dict[str, str]], list[str], list[str]]:
    by_target: dict[tuple[str, str], list[Mapping]] = defaultdict(list)
    for mapping in mappings:
        by_target[(mapping.namespace, mapping.target_secret)].append(mapping)

    values_by_path: dict[str, dict[str, str]] = defaultdict(dict)
    hashes_by_path: dict[str, dict[str, str]] = defaultdict(dict)
    missing: list[str] = []
    recovered: list[str] = []

    def store_mapping_value(mapping: Mapping, value: str) -> None:
        digest = hashlib.sha256(value.encode("utf-8", errors="replace")).hexdigest()
        existing_digest = hashes_by_path[mapping.vault_path].get(mapping.vault_property)
        if existing_digest and existing_digest != digest:
            missing.append(
                f"{mapping.vault_path}: conflicting values for property "
                f"{mapping.vault_property}"
            )
            return

        values_by_path[mapping.vault_path][mapping.vault_property] = value
        hashes_by_path[mapping.vault_path][mapping.vault_property] = digest

    def recover_from_pod_env(mapping: Mapping, reason: str) -> bool:
        if not fallback_pod_env:
            missing.append(reason)
            return False

        found, value = find_pod_env_value(mapping.namespace, mapping.secret_key, dry_run=dry_run)
        if not found:
            missing.append(reason)
            return False

        if not dry_run:
            store_mapping_value(mapping, value)
        else:
            values_by_path[mapping.vault_path].setdefault(mapping.vault_property, "")
        recovered.append(
            f"{mapping.namespace}/{mapping.external_secret}: recovered "
            f"{mapping.secret_key} from running pod env"
        )
        return True

    for (namespace, target_secret), target_mappings in sorted(by_target.items()):
        result = run_capture(["kubectl", "-n", namespace, "get", "secret", target_secret, "-o", "json"])
        if result.returncode != 0:
            for mapping in target_mappings:
                recover_from_pod_env(
                    mapping,
                    f"{namespace}/{target_secret}: missing target for "
                    f"{mapping.external_secret}",
                )
            continue

        secret = json.loads(result.stdout)
        data = secret.get("data") or {}
        for mapping in target_mappings:
            raw = data.get(mapping.secret_key)
            if raw is None:
                recover_from_pod_env(
                    mapping,
                    f"{namespace}/{target_secret}: missing key {mapping.secret_key} "
                    f"for {mapping.external_secret}",
                )
                continue

            value = decode_secret_value(raw)
            store_mapping_value(mapping, value)

    return dict(values_by_path), missing, recovered


def write_vault_path(
    vault_path: str,
    values: dict[str, str],
    *,
    token: str,
    namespace: str,
    pod: str,
) -> None:
    payload = json.dumps(values, ensure_ascii=False)
    result = run_capture(
        [
            "kubectl",
            "exec",
            "-i",
            "-n",
            namespace,
            pod,
            "--",
            "env",
            f"VAULT_TOKEN={token}",
            "vault",
            "kv",
            "put",
            "-format=json",
            vault_path,
            "-",
        ],
        input_text=payload,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip().splitlines()
        message = detail[0] if detail else "unknown Vault write failure"
        raise RuntimeError(f"{vault_path}: {message}")


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Migrate live ESO target Secret values to Vault remoteRef paths"
    )
    parser.add_argument("--store-name", default="vault-store")
    parser.add_argument("--vault-namespace", default=os.environ.get("VAULT_NS", "vault"))
    parser.add_argument("--vault-pod", default=os.environ.get("VAULT_POD", "vault-0"))
    parser.add_argument(
        "--fallback-pod-env",
        action="store_true",
        help="Recover missing target Secret keys from running pod environments without printing values.",
    )
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args()

    mappings, skipped = load_mappings(args.store_name)
    values_by_path, missing, recovered = load_target_secrets(
        mappings,
        fallback_pod_env=args.fallback_pod_env,
        dry_run=args.dry_run,
    )

    property_count = sum(len(values) for values in values_by_path.values())
    print(
        "ExternalSecret migration plan: "
        f"{len(mappings)} mappings, {len(values_by_path)} Vault paths, "
        f"{property_count} properties"
    )

    if skipped:
        print(f"Skipped mappings: {len(skipped)}")
        for item in skipped:
            print(f"  - {item}")

    if missing:
        print(f"Missing source values: {len(missing)}")
        for item in missing:
            print(f"  - {item}")

    if recovered:
        print(f"Recovered from running pod env: {len(recovered)}")
        for item in recovered:
            print(f"  - {item}")

    for path, values in sorted(values_by_path.items()):
        names = ", ".join(sorted(values)) if args.verbose else f"{len(values)} properties"
        action = "Would write" if args.dry_run else "Writing"
        print(f"{action} {path}: {names}")

    if args.dry_run:
        return 0 if values_by_path else 2

    token = os.environ.get("VAULT_TOKEN", "")
    if not token:
        print("VAULT_TOKEN is required unless --dry-run is set", file=sys.stderr)
        return 2

    failures: list[str] = []
    for path, values in sorted(values_by_path.items()):
        try:
            write_vault_path(
                path,
                values,
                token=token,
                namespace=args.vault_namespace,
                pod=args.vault_pod,
            )
        except RuntimeError as exc:
            failures.append(str(exc))

    if failures:
        print(f"Vault writes failed: {len(failures)}", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return 1

    print(f"Vault migration complete: wrote {len(values_by_path)} paths")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
