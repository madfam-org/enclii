"""CI-only, owned Docker fixtures. Never accepts a database/registry target."""
import hashlib
import json
import os
import re
import secrets
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
KERNEL = ROOT / "apps/switchyard-api/internal/k8s"
SOURCE = ROOT / ".janua-stage-source"
SHA = "d327f8df3eb6b1e3a1313a04833856ea1536dfe0"
HOST = "pg-janua-staging-db-rw.project-fixture.svc.cluster.local"
PHASE = "initialization"


def command(args, **kwargs):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, **kwargs).stdout


def main():
    global PHASE
    if os.environ.get("CI") != "true" or os.environ.get("JANUA_STAGE_NATIVE_REQUIRED") != "true":
        raise ValueError("required isolated CI execution only")
    if command(["git", "-C", str(SOURCE), "rev-parse", "HEAD"]).strip() != SHA:
        raise ValueError("foreign fixture source")
    suffix = secrets.token_hex(8)
    network, pg, image = "janua-stage-"+suffix, "janua-stage-pg-"+suffix, "janua-stage-api:"+suffix
    created_network = created_pg = False
    with tempfile.TemporaryDirectory(prefix="janua-stage-native-") as temp:
        folder = Path(temp)
        folder.chmod(0o755)
        ca, key, crt, csr = (folder / n for n in ("ca.crt", "server.key", "server.crt", "server.csr"))
        ca_key = folder / "ca.key"
        ext = folder / "extensions"
        ext.write_text("subjectAltName=DNS:"+HOST+"\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n")
        command(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-subj", "/CN=U-901", "-days", "1", "-keyout", str(ca_key), "-out", str(ca)])
        command(["openssl", "req", "-newkey", "rsa:2048", "-nodes", "-subj", "/CN="+HOST, "-keyout", str(key), "-out", str(csr)])
        command(["openssl", "x509", "-req", "-in", str(csr), "-CA", str(ca), "-CAkey", str(ca_key), "-CAcreateserial", "-days", "1", "-extfile", str(ext), "-out", str(crt)])
        # Read the fixture image's actual group instead of assuming its UID/GID.
        # PostgreSQL accepts root-owned 0640 keys; API receives public CA only.
        pg_gid = int(command(["docker", "run", "--rm", "--network", "none", "--entrypoint", "id", "postgres:18", "-g", "postgres"]).strip())
        if not 0 < pg_gid < 65536:
            raise ValueError("fixture database group refused")
        command(["sudo", "chown", "0:"+str(pg_gid), str(key)])
        key.chmod(0o640)
        ca.chmod(0o644)
        crt.chmod(0o644)
        admin = secrets.token_hex(32)
        owner = secrets.token_hex(32)
        runtime = secrets.token_hex(32)
        try:
            PHASE = "build_image"
            command(["docker", "build", "--build-arg", "VCS_REF="+SHA, "-f", str(SOURCE/"Dockerfile.api"), "-t", image, str(SOURCE)])
            PHASE = "image_identity"
            labels = json.loads(command(["docker", "image", "inspect", "--format", "{{json .Config.Labels}}", image]))
            if labels.get("org.opencontainers.image.source") != "https://github.com/madfam-org/janua" or labels.get("org.opencontainers.image.revision") != SHA:
                raise ValueError("native image source identity mismatch")
            PHASE = "fixture_network"
            created_network = True
            command(["docker", "network", "create", "--internal", network])
            PHASE = "fixture_postgres"
            created_pg = True
            command(["docker", "run", "-d", "--name", pg, "--network", network, "--network-alias", HOST,
                     "-e", "POSTGRES_PASSWORD", "-v", str(folder)+":/tls:ro", "postgres:18",
                     "-c", "ssl=on", "-c", "ssl_cert_file=/tls/server.crt", "-c", "ssl_key_file=/tls/server.key"], env={**os.environ, "POSTGRES_PASSWORD":admin})
            PHASE = "fixture_readiness"
            for _ in range(60):
                ready = subprocess.run(["docker", "exec", pg, "pg_isready", "-U", "postgres"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if ready.returncode == 0:
                    break
                time.sleep(1)
            else:
                raise ValueError("native fixture readiness failed")
            sql = f'''CREATE ROLE enclii_migrator LOGIN PASSWORD '{owner}' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT;
CREATE DATABASE janua_staging OWNER enclii_migrator;
\\connect janua_staging
CREATE ROLE janua_staging_runtime LOGIN PASSWORD '{runtime}' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE TEMPORARY ON DATABASE janua_staging FROM PUBLIC;
GRANT CONNECT ON DATABASE janua_staging TO janua_staging_runtime;
GRANT USAGE ON SCHEMA public TO janua_staging_runtime;
'''
            PHASE = "fixture_roles"
            command(["docker", "exec", "-i", pg, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "postgres"], input=sql)
            ca_path = "/var/run/janua-staging/ca/ca.crt"
            env = {"ENVIRONMENT":"staging", "EMAIL_ENABLED":"false", "AUTO_MIGRATE":"false", "JANUA_APPLY_MIGRATIONS":"false", "SESSION_BRIDGE_ENABLED":"false", "DATABASE_SSL_MODE":"verify-full", "DATABASE_SSL_CA_FILE":ca_path, "SOURCE_SHA":SHA, "CLUSTER_UID":"cluster-fixture", "PYTHONDONTWRITEBYTECODE":"1", "JANUA_STAGE_EXECUTOR_SHA256":hashlib.sha256((KERNEL/"janua_staging_executor.py").read_bytes()).hexdigest(),
                   "JANUA_STAGE_GRANTS":(KERNEL/"janua_staging_runtime_grants.json").read_text(), "JANUA_STAGE_MIGRATIONS":(KERNEL/"janua_staging_migrations.json").read_text()}
            def uri(role, password):
                return "postgresql://"+role+":"+password+"@"+HOST+":5432/janua_staging?sslmode=verify-full&sslrootcert="+ca_path
            env["DATABASE_URL"] = uri("enclii_migrator",owner)
            env["JANUA_STAGE_RUNTIME_DATABASE_URL"] = uri("janua_staging_runtime",runtime)
            # No publish/sign/pin/entrypoint/app lifespan, no external network.
            base = ["docker", "run", "--rm", "--network", network, "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user", "1001:1001", "--tmpfs", "/tmp:rw,nosuid,nodev,size=16m", "-v", str(ca)+":"+ca_path+":ro", "-w", "/app", "--entrypoint", "python"]
            for name in env:
                base += ["-e", name]
            process_env = {**os.environ, **env}
            PHASE = "pretable_refusal"
            # A pre-table partial attempt must not be mistaken for freshness.
            for create, drop in (
                ("CREATE TYPE public.stage_partial_fixture AS ENUM ('U-901')", "DROP TYPE public.stage_partial_fixture"),
                ("CREATE FUNCTION public.stage_partial_fixture() RETURNS integer LANGUAGE sql AS 'SELECT 1'", "DROP FUNCTION public.stage_partial_fixture()"),
                ("CREATE SCHEMA stage_partial_fixture", "DROP SCHEMA stage_partial_fixture"),
                ("ALTER DEFAULT PRIVILEGES FOR ROLE enclii_migrator GRANT SELECT ON TABLES TO janua_staging_runtime", "ALTER DEFAULT PRIVILEGES FOR ROLE enclii_migrator REVOKE SELECT ON TABLES FROM janua_staging_runtime"),
            ):
                prefix = "SET ROLE enclii_migrator; "
                command(["docker", "exec", "-i", pg, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "janua_staging"], input=prefix+create+";")
                refused = subprocess.run(base+[image,"-c",(KERNEL/"janua_staging_executor.py").read_text()], env=process_env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                if refused.returncode == 0 or refused.stdout or refused.stderr.strip() != "Janua staging refused (database_identity).":
                    raise ValueError("partial schema was admitted as fresh")
                command(["docker", "exec", "-i", pg, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "janua_staging"], input=prefix+drop+";")
            PHASE = "fixed_migration"
            receipt = json.loads(command(base+[image,"-c",(KERNEL/"janua_staging_executor.py").read_text()], env=process_env))
            if receipt.get("applied") != 22 or receipt.get("runtime_verified") is not True or receipt.get("sensitive_rows") != 0:
                raise ValueError("native positive receipt incomplete")
            PHASE = "runtime_probe"
            probe = json.loads(command(base+[image,"-c",(ROOT/"tests/janua-stage/native_probe.py").read_text()], env=process_env))
            if probe.get("proof_completed") is not True or probe.get("models_verified") != 11 or probe.get("denied_escalations") != 17 or probe.get("native_magic_claims") != 2 or probe.get("migrated_pair_recheck") is not True:
                raise ValueError("native behavior receipt missing")
            # Same source must fail closed on a nonempty migrated database.
            replay = subprocess.run(base+[image,"-c",(KERNEL/"janua_staging_executor.py").read_text()], env=process_env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            if replay.returncode == 0 or replay.stdout or replay.stderr.strip() != "Janua staging refused (database_identity).":
                raise ValueError("uncertain migration replay was not refused")
            print(json.dumps({"source":SHA,"postgres_major":18,"migration_receipt":receipt,"native_proof":probe,"nonempty_replay_refused":True,"pretable_partial_refusals":4}, separators=(",", ":")))
        finally:
            if created_pg:
                subprocess.run(["docker", "rm", "-f", "-v", pg], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if created_network:
                subprocess.run(["docker", "network", "rm", network], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            subprocess.run(["docker", "image", "rm", image], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    try:
        main()
    except BaseException as error:
        # Only closed phase identifiers and SQLSTATE codes are diagnostic.
        # Never dump command/error bodies, SQL, process env or credentials.
        detail = getattr(error, "stderr", "") or ""
        child_phase = re.search(r"Janua staging refused \(([a-z_]{1,64})\)\.", detail)
        sqlstate = re.search(r"Janua staging SQLSTATE ([A-Z0-9]{5})\.", detail)
        if child_phase:
            sys.stderr.write("Fixed executor phase: "+child_phase.group(1)+".\n")
        if sqlstate:
            sys.stderr.write("Fixed executor SQLSTATE: "+sqlstate.group(1)+".\n")
        sys.stderr.write("Required Janua staging native/image proof failed ("+PHASE+").\n")
        sys.exit(1)
