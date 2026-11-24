"""Fixed initial Janua staging migration; never an API or general SQL runner."""

import ast
import asyncio
import contextlib
import hashlib
import importlib.machinery
import io
import json
import logging
import os
import re
import ssl
import sys
import tempfile
from pathlib import Path
from urllib.parse import parse_qsl, unquote, urlsplit

PROFILE = "janua-staging-alembic-v1"
DATABASE = "janua_staging"
OWNER = "enclii_migrator"
RUNTIME = "janua_staging_runtime"
SHA = re.compile(r"[a-f0-9]{64}\Z")
REVISION = re.compile(r"[a-f0-9]{40}\Z")
# API startup, bootstrap and provider services are never imported. This fixed
# supplied-connection environment runs the actual reviewed revision modules.
ENVIRONMENT = """from alembic import context
connection = context.config.attributes['connection']
context.configure(connection=connection, target_metadata=None, transaction_per_migration=False)
with context.begin_transaction():
    context.run_migrations()
"""


class Sink(io.TextIOBase):
    def write(self, value):
        return len(value)


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError()
            result[key] = value
        return result

    if len(raw) > 262144:
        raise ValueError()
    return json.loads(raw, object_pairs_hook=pairs)


def verify_sources(root, descriptor):
    if (
        set(descriptor)
        != {
            "profile",
            "image",
            "image_source_revision",
            "head",
            "revisions",
            "files",
            "graph_sha256",
        }
        or descriptor["profile"] != PROFILE
    ):
        raise ValueError()
    if not REVISION.fullmatch(descriptor["image_source_revision"]):
        raise ValueError()
    if not re.fullmatch(
        r"ghcr.io/madfam-org/janua-api@sha256:[a-f0-9]{64}", descriptor["image"]
    ):
        raise ValueError()
    claimed = descriptor["graph_sha256"]
    empty = dict(descriptor, graph_sha256="")
    if (
        not SHA.fullmatch(claimed)
        or hashlib.sha256(
            json.dumps(empty, separators=(",", ":"), ensure_ascii=False).encode()
        ).hexdigest()
        != claimed
    ):
        raise ValueError()
    expected = descriptor["files"]
    actual = []
    total = 0
    for directory in ("app", "alembic"):
        for file in (root / directory).rglob("*.py"):
            if (
                file.is_symlink()
                or not file.is_file()
                or root.resolve() not in file.resolve().parents
            ):
                raise ValueError()
            relative = file.relative_to(root).as_posix()
            content = file.read_bytes()
            total += len(content)
            if len(content) > 524288 or total > 12 << 20:
                raise ValueError()
            actual.append(
                {"path": relative, "sha256": hashlib.sha256(content).hexdigest()}
            )
    ini = root / "alembic.ini"
    if ini.is_symlink() or not ini.is_file():
        raise ValueError()
    actual.append(
        {"path": "alembic.ini", "sha256": hashlib.sha256(ini.read_bytes()).hexdigest()}
    )
    actual.sort(key=lambda entry: entry["path"])
    if not 7 <= len(actual) <= 1024 or actual != expected:
        raise ValueError()
    parents = {}
    for entry in actual:
        if (
            not entry["path"].startswith("alembic/versions/")
            or Path(entry["path"]).name == "__init__.py"
        ):
            continue
        if Path(entry["path"]).parent.as_posix() != "alembic/versions":
            raise ValueError()
        tree = ast.parse((root / entry["path"]).read_bytes())
        values = {}
        for node in tree.body:
            target = (
                node.target
                if isinstance(node, ast.AnnAssign)
                else (
                    node.targets[0]
                    if isinstance(node, ast.Assign) and len(node.targets) == 1
                    else None
                )
            )
            if isinstance(target, ast.Name) and target.id in {
                "revision",
                "down_revision",
            }:
                if target.id in values:
                    raise ValueError()
                values[target.id] = ast.literal_eval(node.value)
        if set(values) != {"revision", "down_revision"}:
            raise ValueError()
        revision, parent = values["revision"], values["down_revision"]
        if (
            not isinstance(revision, str)
            or revision in parents
            or not re.fullmatch(r"[A-Za-z0-9_]{1,128}", revision)
        ):
            raise ValueError()
        if parent is not None and (
            not isinstance(parent, str)
            or not re.fullmatch(r"[A-Za-z0-9_]{1,128}", parent)
        ):
            raise ValueError()
        parents[revision] = parent
    heads = set(parents) - set(parents.values())
    if (
        len(heads) != 1
        or next(iter(heads)) != descriptor["head"]
        or len(parents) != descriptor["revisions"]
    ):
        raise ValueError()
    visited = set()
    current = descriptor["head"]
    while current is not None:
        if current in visited or current not in parents:
            raise ValueError()
        visited.add(current)
        current = parents[current]
    if len(visited) != len(parents):
        raise ValueError()

    # Hashing .py while importing stale bytecode would not establish execution
    # provenance. Force image application/revision source compilation in memory.
    original = importlib.machinery.SourceFileLoader.get_code

    def source_only(loader, fullname):
        file = Path(loader.path)
        if root.resolve() in file.resolve().parents:
            return compile(
                loader.get_data(loader.path), loader.path, "exec", dont_inherit=True
            )
        return original(loader, fullname)

    importlib.machinery.SourceFileLoader.get_code = source_only


def validate_connection(database_url, namespace, cluster_uid, ca_file):
    if not re.fullmatch(r"project-[a-f0-9]{8}", namespace) or not re.fullmatch(
        r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", cluster_uid
    ):
        raise ValueError()
    url = urlsplit(database_url)
    host = "pg-janua-staging-db-rw." + namespace + ".svc.cluster.local"
    if (
        url.scheme != "postgresql"
        or url.hostname != host
        or url.port != 5432
        or url.path != "/" + DATABASE
        or unquote(url.username or "") != OWNER
    ):
        raise ValueError()
    password = unquote(url.password or "")
    if (
        len(password) < 16
        or len(password) > 4096
        or any(ord(value) < 32 for value in password)
    ):
        raise ValueError()
    if (
        parse_qsl(url.query, keep_blank_values=True, strict_parsing=True)
        != [("sslmode", "verify-full")]
        or url.fragment
    ):
        raise ValueError()
    transport = ssl.create_default_context(cafile=str(ca_file))
    transport.minimum_version = ssl.TLSVersion.TLSv1_2
    transport.check_hostname = True
    transport.verify_mode = ssl.CERT_REQUIRED
    return transport


def migrate(connection, root, descriptor, cluster_uid):
    from alembic import command
    from alembic.config import Config
    from sqlalchemy import text

    connection.execute(text("SET LOCAL lock_timeout='5s'"))
    connection.execute(text("SET LOCAL statement_timeout='240s'"))
    connection.execute(text("SELECT pg_catalog.pg_advisory_xact_lock(1792966561)"))
    identity = connection.execute(
        text("""SELECT current_database(),current_user,session_user,
 r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication,
 d.datdba=r.oid,EXISTS(SELECT 1 FROM pg_catalog.pg_auth_members WHERE member=r.oid)
 FROM pg_catalog.pg_roles r JOIN pg_catalog.pg_database d ON d.datname=current_database()
 WHERE r.rolname=current_user""")
    ).one()
    if tuple(identity) != (DATABASE, OWNER, OWNER, False, True, False):
        raise ValueError()
    runtime = connection.execute(
        text("""SELECT NOT rolcanlogin OR rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication OR rolinherit,
 pg_catalog.pg_has_role('janua_staging_runtime','enclii_migrator','MEMBER'),
 (SELECT d.datdba=r.oid FROM pg_catalog.pg_database d WHERE d.datname=current_database())
 FROM pg_catalog.pg_roles r WHERE r.rolname='janua_staging_runtime'""")
    ).one()
    if tuple(runtime) != (False, False, False):
        raise ValueError()
    empty = connection.execute(
        text("""SELECT
 (SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema'),
 (SELECT count(*) FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema'),
 (SELECT count(*) FROM pg_catalog.pg_namespace WHERE nspname NOT LIKE 'pg_%' AND nspname NOT IN ('information_schema','public'))""")
    ).one()
    if tuple(empty) != (0, 0, 0):
        raise ValueError()
    with tempfile.TemporaryDirectory(prefix="janua-alembic-", dir="/tmp") as scratch:
        Path(scratch, "env.py").write_text(ENVIRONMENT)
        config = Config()
        config.set_main_option("script_location", scratch)
        config.set_main_option("version_locations", str(root / "alembic" / "versions"))
        config.attributes["connection"] = connection
        command.upgrade(config, descriptor["head"])
    versions = (
        connection.execute(text("SELECT version_num FROM public.alembic_version"))
        .scalars()
        .all()
    )
    if versions != [descriptor["head"]]:
        raise ValueError()
    for table in ("users", "organizations", "oauth_clients", "sessions", "api_keys"):
        if (
            connection.execute(
                text('SELECT count(*) FROM public."' + table + '"')
            ).scalar_one()
            != 0
        ):
            raise ValueError()
    connection.execute(
        text(
            "REVOKE UPDATE,DELETE ON public.organization_authentication_policy_changes,public.email_events FROM janua_staging_runtime"
        )
    )
    connection.execute(
        text(
            "REVOKE DELETE ON public.email_send_audits,public.payment_mail_dispatches FROM janua_staging_runtime"
        )
    )
    connection.execute(
        text("REVOKE ALL ON public.alembic_version FROM janua_staging_runtime")
    )
    connection.execute(
        text("GRANT SELECT ON public.alembic_version TO janua_staging_runtime")
    )
    connection.execute(
        text(
            "CREATE TABLE public.enclii_janua_stage_schema_provenance(singleton integer PRIMARY KEY CHECK(singleton=1),profile text NOT NULL,cluster_uid text NOT NULL,source_revision text NOT NULL,graph_sha256 text NOT NULL,head text NOT NULL)"
        )
    )
    connection.execute(
        text(
            "INSERT INTO public.enclii_janua_stage_schema_provenance VALUES(1,:profile,:cluster,:source,:graph,:head)"
        ),
        {
            "profile": PROFILE,
            "cluster": cluster_uid,
            "source": descriptor["image_source_revision"],
            "graph": descriptor["graph_sha256"],
            "head": descriptor["head"],
        },
    )
    connection.execute(
        text(
            "REVOKE ALL ON public.enclii_janua_stage_schema_provenance FROM PUBLIC,janua_staging_runtime"
        )
    )
    authority = connection.execute(
        text("""SELECT
 pg_catalog.has_schema_privilege('janua_staging_runtime','public','CREATE'),
 pg_catalog.has_database_privilege('janua_staging_runtime',current_database(),'CREATE,TEMPORARY'),
 pg_catalog.has_table_privilege('janua_staging_runtime','public.organization_authentication_policy_changes','UPDATE,DELETE,TRUNCATE,TRIGGER,REFERENCES'),
 pg_catalog.has_table_privilege('janua_staging_runtime','public.email_events','UPDATE,DELETE,TRUNCATE,TRIGGER,REFERENCES'),
 pg_catalog.has_table_privilege('janua_staging_runtime','public.email_send_audits','DELETE,TRUNCATE,TRIGGER,REFERENCES'),
 pg_catalog.has_table_privilege('janua_staging_runtime','public.alembic_version','INSERT,UPDATE,DELETE,TRUNCATE,TRIGGER,REFERENCES'),
 pg_catalog.has_table_privilege('janua_staging_runtime','public.enclii_janua_stage_schema_provenance','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,TRIGGER,REFERENCES'),
 EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','S','v','m','f') AND c.relowner<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname='enclii_migrator'))""")
    ).one()
    if any(authority):
        raise ValueError()
    catalog = connection.execute(
        text("""SELECT coalesce(string_agg(item,E'\n' ORDER BY item),'') FROM (
 SELECT 'column:'||c.relname||':'||a.attname||':'||pg_catalog.format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull::text||':'||coalesce(pg_catalog.pg_get_expr(d.adbin,d.adrelid),'') item
 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
 LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum WHERE n.nspname='public' AND c.relkind IN ('r','p') AND a.attnum>0 AND NOT a.attisdropped
 UNION ALL SELECT 'constraint:'||conrelid::regclass::text||':'||conname||':'||pg_catalog.pg_get_constraintdef(oid) FROM pg_catalog.pg_constraint WHERE connamespace='public'::regnamespace
 UNION ALL SELECT 'index:'||tablename||':'||indexname||':'||indexdef FROM pg_catalog.pg_indexes WHERE schemaname='public'
 UNION ALL SELECT 'acl:'||c.relname||':'||coalesce(c.relacl::text,'') FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','S','v','m','f')
 ) catalog""")
    ).scalar_one()
    return hashlib.sha256(catalog.encode()).hexdigest()


async def execute(root, descriptor, database_url, namespace, cluster_uid, ca_file):
    verify_sources(root, descriptor)
    transport = validate_connection(database_url, namespace, cluster_uid, ca_file)
    from sqlalchemy.engine import make_url
    from sqlalchemy.ext.asyncio import create_async_engine
    from sqlalchemy.pool import NullPool

    url = (
        make_url(database_url)
        .set(drivername="postgresql+asyncpg")
        .difference_update_query(["sslmode"])
    )
    engine = create_async_engine(
        url,
        echo=False,
        poolclass=NullPool,
        connect_args={"ssl": transport, "statement_cache_size": 0, "timeout": 5},
    )
    try:
        async with engine.begin() as connection:
            catalog = await connection.run_sync(
                lambda sync: migrate(sync, root, descriptor, cluster_uid)
            )
    finally:
        await engine.dispose()
    return {
        "profile": PROFILE,
        "image_source_revision": descriptor["image_source_revision"],
        "graph_sha256": descriptor["graph_sha256"],
        "head": descriptor["head"],
        "applied": descriptor["revisions"],
        "pending": 0,
        "lineage": "FRESH_EMPTY",
        "database": DATABASE,
        "owner_role": OWNER,
        "cluster_uid": cluster_uid,
        "catalog_sha256": catalog,
        "principal_rows": 0,
    }


def main():
    try:
        logging.disable(logging.CRITICAL)
        with contextlib.redirect_stdout(Sink()), contextlib.redirect_stderr(Sink()):
            mounted = Path("/var/run/janua-migration")
            descriptor = strict_json((mounted / "descriptor.json").read_bytes())
            custody = strict_json((mounted / "owner.json").read_bytes())
            if set(custody) != {"database_url", "namespace", "cluster_uid"}:
                raise ValueError()
            if (
                custody["namespace"] != os.environ["EXPECTED_NAMESPACE"]
                or custody["namespace"] != os.environ["POD_NAMESPACE"]
                or custody["cluster_uid"] != os.environ["EXPECTED_CLUSTER_UID"]
                or descriptor["graph_sha256"] != os.environ["EXPECTED_GRAPH_SHA256"]
                or descriptor["head"] != os.environ["EXPECTED_HEAD"]
                or descriptor["image"] != os.environ["EXPECTED_IMAGE"]
                or descriptor["image_source_revision"]
                != os.environ["EXPECTED_IMAGE_SOURCE_REVISION"]
            ):
                raise ValueError()
            receipt = asyncio.run(
                execute(
                    Path("/app"),
                    descriptor,
                    custody["database_url"],
                    custody["namespace"],
                    custody["cluster_uid"],
                    mounted / "ca.crt",
                )
            )
        print(json.dumps(receipt, separators=(",", ":")))
    except BaseException:  # noqa: BLE001 - never disclose credential-bearing tracebacks
        sys.stderr.write(
            "Fixed Janua staging migration refused; inspect the same operation.\n"
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
