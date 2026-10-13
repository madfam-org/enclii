#!/usr/bin/env python3
"""Fixed disposable PostgreSQL18/TLS proof; no caller URL, SQL or fixture selectors.

Docker exec is restricted to the exact freshly owned synthetic test container.
This helper never reads platform custody or contacts a production database.
"""
from pathlib import Path
import datetime
import json
import os
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse


def main():
    if len(sys.argv) != 1:
        raise SystemExit("fixed proof accepts no arguments")
    work = Path(__file__).resolve().parents[1] / "apps/switchyard-api"
    tmp = Path(tempfile.mkdtemp(prefix="enclii-binding-native-"))
    tmp.chmod(0o700)
    cid = None
    labels = {"codex.owner": "devops_stabilization", "codex.purpose": "synthetic-workload-binding-proof"}
    receipt = {"version": "enclii-target-native-proof-v2", "productionAccess": False}
    sensitive = []

    def run(args, **kwargs):
        result = subprocess.run(args, capture_output=True, check=False, **kwargs)
        if result.returncode:
            raise RuntimeError("fixed fixture command failed: " + os.path.basename(args[0]))
        return result.stdout

    try:
        ca, key, server_key = tmp / "ca.crt", tmp / "ca.key", tmp / "server.key"
        csr, certificate, extensions = tmp / "server.csr", tmp / "server.crt", tmp / "ext.cnf"
        run(["openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048", "-keyout", str(key), "-out", str(ca), "-days", "1", "-subj", "/CN=binding-fixture-ca"])
        run(["openssl", "req", "-nodes", "-newkey", "rsa:2048", "-keyout", str(server_key), "-out", str(csr), "-subj", "/CN=binding-fixture"])
        extensions.write_text("subjectAltName=IP:127.0.0.1,DNS:localhost\n")
        run(["openssl", "x509", "-req", "-in", str(csr), "-CA", str(ca), "-CAkey", str(key), "-CAcreateserial", "-out", str(certificate), "-days", "1", "-extfile", str(extensions)])
        for file in [key, server_key]:
            file.chmod(0o600)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        suffix = secrets.token_hex(4)
        database = "workload_bindings_" + suffix + "_sim"
        admin_password, owner_password, runtime_password = [secrets.token_hex(24) for _ in range(3)]
        sensitive += [admin_password, owner_password, runtime_password]
        env = os.environ.copy()
        env["POSTGRES_PASSWORD"] = admin_password
        cid = run(["docker", "run", "--detach", "--name", "codex-binding-proof-" + suffix, "--label", "codex.owner=devops_stabilization", "--label", "codex.purpose=synthetic-workload-binding-proof", "--cpus", "1", "--memory", "512m", "--tmpfs", "/var/lib/postgresql:size=256m,mode=1777", "--publish", f"127.0.0.1:{port}:5432", "--env", "POSTGRES_PASSWORD", "--mount", f"type=bind,src={tmp},dst=/fixture-tls,readonly", "postgres:18"], env=env).decode().strip()
        for _ in range(120):
            result = subprocess.run(["docker", "exec", cid, "pg_isready", "-h", "127.0.0.1", "-U", "postgres"], capture_output=True, check=False)
            if result.returncode == 0:
                break
            time.sleep(.25)
        else:
            raise RuntimeError("owned fixture startup unavailable")
        run(["docker", "exec", "--user", "0", cid, "sh", "-c", "cp /fixture-tls/server.key /tmp/binding-server.key && cp /fixture-tls/server.crt /tmp/binding-server.crt && chown postgres:postgres /tmp/binding-server.key /tmp/binding-server.crt && chmod 600 /tmp/binding-server.key"])
        sql = (f"CREATE ROLE binding_owner LOGIN PASSWORD '{owner_password}' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION INHERIT;\n"
               f"CREATE ROLE binding_runtime LOGIN PASSWORD '{runtime_password}' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT;\n"
               f"CREATE DATABASE {database} OWNER binding_owner;\nREVOKE TEMPORARY ON DATABASE {database} FROM PUBLIC;\n"
               "ALTER SYSTEM SET ssl='on';\nALTER SYSTEM SET ssl_cert_file='/tmp/binding-server.crt';\nALTER SYSTEM SET ssl_key_file='/tmp/binding-server.key';\nSELECT pg_reload_conf();\n")
        run(["docker", "exec", "-i", cid, "psql", "-X", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1"], input=sql.encode())

        def dsn(role, password):
            query = urllib.parse.urlencode({"sslmode": "verify-full", "sslrootcert": str(ca)})
            return f"postgresql://{role}:{password}@127.0.0.1:{port}/{database}?{query}"

        owner_url, runtime_url = dsn("binding_owner", owner_password), dsn("binding_runtime", runtime_password)
        sensitive += [owner_url, runtime_url]
        test_env = os.environ.copy()
        test_env.update({"ENCLII_TARGET_OWNER_PROOF_URL": owner_url, "ENCLII_TARGET_RUNTIME_PROOF_URL": runtime_url, "GOMAXPROCS": "2"})
        result = subprocess.run(["go", "test", "-p", "1", "-race", "-tags", "workload_target_postgres", "./internal/workloadtargets", "-run", "^TestNativeTarget", "-count=1", "-json"], cwd=work, env=test_env, capture_output=True, check=False)
        output = (result.stdout + result.stderr).decode(errors="replace")
        for value in sensitive:
            output = output.replace(value, "<fixture-custody-redacted>")
        print(output, end="")
        receipt["testExitCode"] = result.returncode
        receipt["fixtureImage"] = json.loads(run(["docker", "inspect", "--format", "{{json .Image}}", cid]))
        receipt["clientTLSMode"] = "verify-full"
        receipt["status"] = "passed" if result.returncode == 0 else "failed"
    except RuntimeError as error:
        receipt["status"] = "fixture_failed"
        receipt["failureCode"] = str(error)
    finally:
        if cid:
            actual = json.loads(run(["docker", "inspect", "--format", "{{json .Config.Labels}}", cid]))
            if all(actual.get(key) == value for key, value in labels.items()):
                run(["docker", "rm", "--force", "--volumes", cid])
                receipt["ownedContainerRemoved"] = True
            else:
                receipt["status"] = "cleanup_ownership_conflict"
        if cid is None or receipt.get("ownedContainerRemoved"):
            shutil.rmtree(tmp)
            receipt["temporaryFixtureCredentialsRemoved"] = True
        receipt["finishedAt"] = datetime.datetime.now(datetime.UTC).isoformat()
        print(json.dumps(receipt))
    raise SystemExit(0 if receipt.get("status") == "passed" and receipt.get("ownedContainerRemoved") else 1)


if __name__ == "__main__":
    main()
