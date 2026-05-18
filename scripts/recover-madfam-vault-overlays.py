#!/usr/bin/env python3
"""Recover MADFAM service-specific Vault overlays after a Vault rebootstrap.

This script handles values that cannot be recovered by the generic
ExternalSecret target migrator because they live in split Kubernetes Secrets,
literal deployment config, generated service tokens, or provider registration
responses. Secret values are never printed.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Iterable
from typing import Any

_POD_ENV_CACHE: dict[str, dict[str, str]] = {}


def run_capture(
    args: list[str],
    *,
    input_text: str | None = None,
    timeout: int = 30,
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
    result = run_capture(["kubectl", "--request-timeout=8s", *args, "-o", "json"])
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or result.stdout.strip())
    return json.loads(result.stdout)


def decode_data_value(raw: str) -> str:
    return base64.b64decode(raw).decode("utf-8", errors="replace")


def encode_data_value(value: str) -> str:
    return base64.b64encode(value.encode("utf-8")).decode("ascii")


def read_secret(namespace: str, name: str) -> dict[str, str]:
    secret = kubectl_json(["-n", namespace, "get", "secret", name])
    return {
        key: decode_data_value(value)
        for key, value in (secret.get("data") or {}).items()
    }


def read_secret_key(namespace: str, name: str, key: str) -> str:
    values = read_secret(namespace, name)
    if key not in values:
        raise RuntimeError(f"{namespace}/{name}: missing key {key}")
    return values[key]


def deployment_literal_env(
    namespace: str,
    deployments: Iterable[str],
    key: str,
) -> str | None:
    for deployment in deployments:
        item = kubectl_json(["-n", namespace, "get", "deployment", deployment])
        containers = item.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
        for container in containers:
            for env in container.get("env") or []:
                if env.get("name") == key and env.get("value") is not None:
                    return env["value"]
    return None


def find_pod_env(namespace: str, key: str) -> str | None:
    if namespace in _POD_ENV_CACHE:
        return _POD_ENV_CACHE[namespace].get(key)

    pods = kubectl_json(["-n", namespace, "get", "pods"])
    env_values: dict[str, str] = {}
    for pod in pods.get("items", []):
        if pod.get("status", {}).get("phase") != "Running":
            continue
        pod_name = pod.get("metadata", {}).get("name", "")
        if "-admin-" in pod_name:
            continue
        for container in pod.get("spec", {}).get("containers", []):
            container_name = container.get("name", "")
            if container_name == "admin":
                continue
            result = run_capture(
                [
                    "kubectl",
                    "--request-timeout=8s",
                    "-n",
                    namespace,
                    "exec",
                    pod_name,
                    "-c",
                    container_name,
                    "--",
                    "printenv",
                ],
                timeout=8,
            )
            if result.returncode == 0:
                for line in result.stdout.splitlines():
                    env_key, sep, value = line.partition("=")
                    if sep and env_key not in env_values:
                        env_values[env_key] = value
    _POD_ENV_CACHE[namespace] = env_values
    return env_values.get(key)


def random_token() -> str:
    return secrets.token_urlsafe(48)


def derive_redis_password(redis_url: str) -> str:
    parsed = urllib.parse.urlparse(redis_url)
    return urllib.parse.unquote(parsed.password or "")


def redis_url_with_password(redis_url: str, password: str) -> str:
    parsed = urllib.parse.urlsplit(redis_url)
    if not parsed.hostname:
        raise RuntimeError("redis URL is missing a hostname")
    port = f":{parsed.port}" if parsed.port else ""
    username = parsed.username or ""
    userinfo = f"{urllib.parse.quote(username, safe='')}:" if username else ":"
    hostname = parsed.hostname
    if ":" in hostname and not hostname.startswith("["):
        hostname = f"[{hostname}]"
    netloc = f"{userinfo}{urllib.parse.quote(password, safe='')}@{hostname}{port}"
    return urllib.parse.urlunsplit(
        (parsed.scheme or "redis", netloc, parsed.path or "/0", parsed.query, parsed.fragment)
    )


def read_vault_path(
    vault_path: str,
    *,
    namespace: str,
    pod: str,
    token_file: str,
) -> dict[str, str]:
    result = run_capture(
        [
            "kubectl",
            "exec",
            "-n",
            namespace,
            pod,
            "--",
            "sh",
            "-c",
            'VAULT_TOKEN="$(cat "$1")" vault kv get -format=json "$2"',
            "sh",
            token_file,
            vault_path,
        ],
        timeout=20,
    )
    if result.returncode != 0:
        detail = result.stderr or result.stdout
        if "No value found" in detail:
            return {}
        raise RuntimeError(f"{vault_path}: failed to read existing Vault data")
    data = json.loads(result.stdout)
    return dict(data.get("data", {}).get("data", {}) or {})


def write_vault_path(
    vault_path: str,
    updates: dict[str, str],
    *,
    namespace: str,
    pod: str,
    token_file: str,
) -> None:
    existing = read_vault_path(vault_path, namespace=namespace, pod=pod, token_file=token_file)
    merged = {**existing, **updates}
    result = run_capture(
        [
            "kubectl",
            "exec",
            "-i",
            "-n",
            namespace,
            pod,
            "--",
            "sh",
            "-c",
            'VAULT_TOKEN="$(cat "$1")" vault kv put -format=json "$2" -',
            "sh",
            token_file,
            vault_path,
        ],
        input_text=json.dumps(merged, ensure_ascii=False),
        timeout=20,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip().splitlines()
        message = detail[0] if detail else "unknown Vault write failure"
        raise RuntimeError(f"{vault_path}: {message}")


def apply_secret(namespace: str, name: str, updates: dict[str, str]) -> None:
    current = read_secret(namespace, name)
    merged = {**current, **updates}
    manifest = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {"name": name, "namespace": namespace},
        "type": "Opaque",
        "data": {key: encode_data_value(value) for key, value in sorted(merged.items())},
    }
    result = run_capture(["kubectl", "apply", "-f", "-"], input_text=json.dumps(manifest))
    if result.returncode != 0:
        raise RuntimeError(f"{namespace}/{name}: failed to apply Kubernetes Secret")


def janua_register_phynd_client(internal_api_key: str, register_url: str) -> dict[str, str]:
    timestamp = time.strftime("%Y%m%d%H%M%S", time.gmtime())
    payload = {
        "name": f"Phynd CRM Production Vault {timestamp}",
        "description": f"Production Phynd CRM OAuth client rotated during Vault rebootstrap {timestamp}",
        "redirect_uris": [
            "https://crm.phynd.app/api/auth/callback/janua",
            "https://phynd.app/api/auth/callback/janua",
            "https://www.phynd.app/api/auth/callback/janua",
            "https://crm.madfam.io/api/auth/callback/janua",
        ],
        "grant_types": ["authorization_code", "refresh_token"],
        "allowed_scopes": ["openid", "profile", "email"],
        "client_key": f"phynd-crm-production-vault-{timestamp}",
        "audience": "janua.dev",
        "allowed_origins": [
            "https://crm.phynd.app",
            "https://phynd.app",
            "https://www.phynd.app",
            "https://crm.madfam.io",
        ],
        "post_logout_redirect_uris": [
            "https://crm.phynd.app/login",
            "https://phynd.app/login",
            "https://crm.madfam.io/login",
        ],
        "is_confidential": True,
    }
    request = urllib.request.Request(
        register_url,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Content-Type": "application/json",
            "X-Internal-API-Key": internal_api_key,
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            body = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Janua OAuth client registration failed: HTTP {exc.code}: {body[:160]}")

    client_id = body.get("client_id")
    client_secret = body.get("client_secret")
    if not client_id or not client_secret:
        raise RuntimeError("Janua OAuth client registration response lacked client_id/client_secret")
    return {"client_id": client_id, "client_secret": client_secret}


def require(value: str | None, label: str) -> str:
    if value is None:
        raise RuntimeError(f"missing required recovery value: {label}")
    return value


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vault-namespace", default=os.environ.get("VAULT_NS", "vault"))
    parser.add_argument("--vault-pod", default=os.environ.get("VAULT_POD", "vault-0"))
    parser.add_argument(
        "--vault-token-file",
        default=os.environ.get("VAULT_TOKEN_FILE", "/tmp/madfam-vault-bootstrap-token"),
    )
    parser.add_argument("--skip-janua-client", action="store_true")
    parser.add_argument(
        "--janua-register-url",
        default=os.environ.get(
            "JANUA_REGISTER_URL",
            "https://auth.madfam.io/api/v1/oauth/clients/register",
        ),
        help="Janua OAuth client registration endpoint.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help=(
            "Read current state and report target patches without writing Vault, "
            "patching K8s Secrets, or registering Janua clients."
        ),
    )
    args = parser.parse_args()

    patches: list[tuple[str, dict[str, str]]] = []

    ceq_r2 = read_secret("ceq", "r2-credentials")
    ceq_fal = read_secret("ceq", "ceq-fal-credentials")
    ceq_redis_url = find_pod_env("ceq", "REDIS_URL")
    ceq_patch = {
        "DATABASE_URL": require(find_pod_env("ceq", "DATABASE_URL"), "ceq DATABASE_URL"),
        "REDIS_URL": require(ceq_redis_url, "ceq REDIS_URL"),
        "REDIS_PASSWORD": derive_redis_password(require(ceq_redis_url, "ceq REDIS_URL")),
        "R2_ENDPOINT": ceq_r2["R2_ENDPOINT_URL"],
        "R2_ACCESS_KEY": ceq_r2["R2_ACCESS_KEY_ID"],
        "R2_SECRET_KEY": ceq_r2["R2_SECRET_ACCESS_KEY"],
        "R2_BUCKET_NAME": ceq_r2["R2_BUCKET_NAME"],
        "JOB_COMPLETION_CALLBACK_TOKEN": find_pod_env("ceq", "JOB_COMPLETION_CALLBACK_TOKEN")
        or random_token(),
        "JOB_WEBHOOK_SECRET": find_pod_env("ceq", "JOB_WEBHOOK_SECRET") or random_token(),
        "FURNACE_API_KEY": ceq_fal["FAL_API_KEY"],
    }
    patches.append(("secret/ceq", ceq_patch))

    dhanam_r2 = read_secret("dhanam", "r2-credentials")
    dhanam_billing = read_secret("dhanam", "dhanam-billing-secrets")
    cloudflare = read_secret("enclii", "enclii-cloudflare-credentials")
    dhanam_secret_keys = [
        "DATABASE_URL",
        "REDIS_URL",
        "JWT_SECRET",
        "JWT_REFRESH_SECRET",
        "OIDC_CLIENT_SECRET",
        "COTIZA_WEBHOOK_SECRET",
        "METAMAP_CLIENT_ID",
        "METAMAP_CLIENT_SECRET",
        "METAMAP_WEBHOOK_SECRET",
        "FEDERATION_API_TOKEN",
        "METAMAP_FLOW_ID",
        "CHECKOUT_ALLOWED_HOSTS",
        "SMTP_HOST",
        "SMTP_PORT",
        "SMTP_USER",
        "SMTP_PASSWORD",
        "EMAIL_FROM",
        "BANXICO_API_TOKEN",
        "POSTHOG_API_KEY",
        "ENCRYPTION_KEY",
        "NEXTAUTH_SECRET",
        "SENTRY_DSN",
    ]
    dhanam_env = {key: find_pod_env("dhanam", key) for key in dhanam_secret_keys}
    nextauth_secret = dhanam_env.get("NEXTAUTH_SECRET") or random_token()
    dhanam_patch = {
        "database_url": require(dhanam_env.get("DATABASE_URL"), "dhanam DATABASE_URL"),
        "redis_url": require(dhanam_env.get("REDIS_URL"), "dhanam REDIS_URL"),
        "jwt_secret": require(dhanam_env.get("JWT_SECRET"), "dhanam JWT_SECRET"),
        "jwt_refresh_secret": dhanam_env.get("JWT_REFRESH_SECRET") or random_token(),
        "oidc_client_id": require(
            deployment_literal_env("dhanam", ["dhanam-web"], "NEXT_PUBLIC_OIDC_CLIENT_ID"),
            "dhanam NEXT_PUBLIC_OIDC_CLIENT_ID",
        ),
        "oidc_client_secret": require(
            dhanam_env.get("OIDC_CLIENT_SECRET"), "dhanam OIDC_CLIENT_SECRET"
        ),
        "oidc_issuer": require(
            deployment_literal_env("dhanam", ["dhanam-web", "dhanam-api"], "NEXT_PUBLIC_OIDC_ISSUER")
            or deployment_literal_env("dhanam", ["dhanam-api"], "JANUA_ISSUER"),
            "dhanam OIDC issuer",
        ),
        "nextauth_secret": nextauth_secret,
        "stripe_secret_key": dhanam_billing["STRIPE_MX_SECRET_KEY"],
        "stripe_webhook_secret": dhanam_billing["STRIPE_MX_WEBHOOK_SECRET"],
        "sendgrid_api_key": find_pod_env("dhanam", "SENDGRID_API_KEY") or "",
        "cotiza_webhook_secret": dhanam_env.get("COTIZA_WEBHOOK_SECRET") or random_token(),
        "metamap_client_id": dhanam_env.get("METAMAP_CLIENT_ID") or "",
        "metamap_client_secret": dhanam_env.get("METAMAP_CLIENT_SECRET") or "",
        "metamap_webhook_secret": dhanam_env.get("METAMAP_WEBHOOK_SECRET") or random_token(),
        "federation_api_token": dhanam_env.get("FEDERATION_API_TOKEN") or random_token(),
        "metamap_flow_id": dhanam_env.get("METAMAP_FLOW_ID") or "",
        "checkout_allowed_hosts": dhanam_env.get("CHECKOUT_ALLOWED_HOSTS") or "app.dhan.am,dhan.am,www.dhan.am",
        "smtp_host": dhanam_env.get("SMTP_HOST") or "",
        "smtp_port": dhanam_env.get("SMTP_PORT") or "587",
        "smtp_user": dhanam_env.get("SMTP_USER") or "",
        "smtp_password": dhanam_env.get("SMTP_PASSWORD") or "",
        "email_from": dhanam_env.get("EMAIL_FROM") or "noreply@dhan.am",
        "banxico_api_token": dhanam_env.get("BANXICO_API_TOKEN") or "",
        "r2_access_key_id": dhanam_r2["R2_ACCESS_KEY_ID"],
        "r2_secret_access_key": dhanam_r2["R2_SECRET_ACCESS_KEY"],
        "r2_endpoint": dhanam_r2["R2_ENDPOINT_URL"],
        "cloudflare_api_token": cloudflare["api-token"],
        "posthog_api_key": dhanam_env.get("POSTHOG_API_KEY") or "",
        "next_public_posthog_key": find_pod_env("dhanam", "NEXT_PUBLIC_POSTHOG_KEY") or "",
        "session_secret": nextauth_secret,
        "encryption_key": require(dhanam_env.get("ENCRYPTION_KEY"), "dhanam ENCRYPTION_KEY"),
        "sentry_dsn": dhanam_env.get("SENTRY_DSN") or "",
    }
    patches.append(("secret/dhanam", dhanam_patch))

    billing_live_patch = {
        "STRIPE_WEBHOOK_SECRET": dhanam_billing["STRIPE_MX_WEBHOOK_SECRET"],
        "CONEKTA_PRIVATE_KEY": dhanam_billing.get("CONEKTA_PRIVATE_KEY", ""),
        "CONEKTA_PUBLIC_KEY": dhanam_billing.get("CONEKTA_PUBLIC_KEY", ""),
        "CONEKTA_WEBHOOK_SIGNING_KEY": dhanam_billing.get("CONEKTA_WEBHOOK_SIGNING_KEY", ""),
        "BILLING_WEBHOOK_SECRET": dhanam_billing.get("BILLING_WEBHOOK_SECRET") or random_token(),
        "PHYND_ENGAGEMENT_EVENTS_SECRET": dhanam_billing.get("PHYND_ENGAGEMENT_EVENTS_SECRET")
        or random_token(),
        "MADFAM_EVENTS_WEBHOOK_SECRET": dhanam_billing.get("MADFAM_EVENTS_WEBHOOK_SECRET")
        or random_token(),
    }
    if args.dry_run:
        print(
            "Would patch Kubernetes secret dhanam/dhanam-billing-secrets: "
            + ", ".join(sorted(billing_live_patch))
        )
    else:
        apply_secret("dhanam", "dhanam-billing-secrets", billing_live_patch)
        print(
            "Patched Kubernetes secret dhanam/dhanam-billing-secrets: "
            + ", ".join(sorted(billing_live_patch))
        )

    phynd_current = read_secret("phynd-crm", "phynd-crm-secrets")
    phynd_db = read_secret("project-c72121bb", "pg-phynd-crm-postgres2-a9d984cc-app")
    phynd_client: dict[str, str] = {}
    if args.dry_run and not args.skip_janua_client:
        phynd_client = {"client_id": "__dry_run__", "client_secret": "__dry_run__"}
        print("Would register Janua OAuth client for Phynd CRM")
    elif not args.skip_janua_client:
        janua_key = read_secret_key("dhanam", "dhanam-janua-secrets", "JANUA_ADMIN_KEY")
        phynd_client = janua_register_phynd_client(janua_key, args.janua_register_url)
        print(f"Registered Janua OAuth client for Phynd CRM: {phynd_client['client_id']}")
    phynd_patch = {
        "database_url": phynd_db.get("uri") or phynd_db["fqdn-uri"],
        "redis_url": phynd_current["REDIS_URL"],
        "auth_secret": phynd_current.get("AUTH_SECRET") or random_token(),
        "auth_janua_issuer": "https://auth.madfam.io",
        "auth_janua_client_id": phynd_client.get("client_id")
        or require(find_pod_env("phynd-crm", "AUTH_JANUA_CLIENT_ID"), "phynd AUTH_JANUA_CLIENT_ID"),
        "auth_janua_client_secret": phynd_client.get("client_secret")
        or require(
            find_pod_env("phynd-crm", "AUTH_JANUA_CLIENT_SECRET"),
            "phynd AUTH_JANUA_CLIENT_SECRET",
        ),
        "janua_api_url": "https://auth.madfam.io",
        "janua_telemetry_api_url": "https://auth.madfam.io",
        "dhanam_api_url": "https://api.dhan.am",
        "cotiza_api_url": "https://api.cotiza.studio",
        "pravara_base_url": "https://mes-api.madfam.io",
        "forj_api_url": "https://forj.design",
        "next_public_app_url": "https://crm.phynd.app",
        "node_env": "production",
    }
    patches.append(("secret/phynd-crm", phynd_patch))

    converge_current = read_secret("converge-dash", "converge-dash-secrets")
    converge_patch = {
        "redis_url": redis_url_with_password(
            require(converge_current.get("REDIS_URL"), "converge REDIS_URL"),
            read_secret_key("data", "redis-auth", "redis-password"),
        ),
        "dhanam_api_token": find_pod_env("converge-dash", "DHANAM_API_TOKEN") or random_token(),
        "phynd_crm_api_token": find_pod_env("converge-dash", "PHYND_CRM_API_TOKEN")
        or random_token(),
        "selva_api_token": find_pod_env("converge-dash", "SELVA_API_TOKEN") or random_token(),
        "enclii_api_token": find_pod_env("converge-dash", "ENCLII_API_TOKEN") or random_token(),
    }
    patches.append(("secret/converge-dash", converge_patch))

    for path, updates in patches:
        if args.dry_run:
            print(f"Would patch Vault {path}: {', '.join(sorted(updates))}")
            continue
        write_vault_path(
            path,
            updates,
            namespace=args.vault_namespace,
            pod=args.vault_pod,
            token_file=args.vault_token_file,
        )
        print(f"Patched Vault {path}: {', '.join(sorted(updates))}")

    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"Recovery overlay failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
