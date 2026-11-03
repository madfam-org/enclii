"""Fixed, source-bound Janua staging migration. Never a general SQL/job API."""
import contextlib
import hashlib
import io
import json
import logging
import os
import re
import secrets
import sys
from pathlib import Path


def digest(value):
    return hashlib.sha256(value).hexdigest()


def refuse(condition):
    if not condition:
        raise ValueError("closed staging contract refused")


def catalog(cursor):
    # Schema metadata only; no entity values, token material or DSNs.
    cursor.execute("""
      SELECT json_build_object(
        'relation_acl',(SELECT json_agg(x ORDER BY x.relname) FROM
          (SELECT c.relname,c.relacl::text FROM pg_class c
             JOIN pg_namespace n ON n.oid=c.relnamespace
             WHERE n.nspname='public' AND c.relkind IN ('r','p','S')) x),
        'type_acl',(SELECT json_agg(x ORDER BY x.typname) FROM
          (SELECT t.typname,t.typacl::text FROM pg_type t
             JOIN pg_namespace n ON n.oid=t.typnamespace
             WHERE n.nspname='public' AND t.typtype='e') x),
        'functions',(SELECT json_agg(x ORDER BY x.proname) FROM
          (SELECT p.proname,p.proacl::text,pg_get_functiondef(p.oid) AS definition
             FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
             WHERE n.nspname='public') x),
        'columns',(SELECT json_agg(x ORDER BY x.table_name,x.ordinal_position) FROM
          (SELECT table_name,column_name,ordinal_position,data_type,udt_name,
                  is_nullable,column_default FROM information_schema.columns
             WHERE table_schema='public') x),
        'constraints',(SELECT json_agg(x ORDER BY x.relname,x.conname) FROM
          (SELECT c.relname,k.conname,k.contype,pg_get_constraintdef(k.oid) AS definition
             FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid
             JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public') x),
        'indexes',(SELECT json_agg(x ORDER BY x.tablename,x.indexname) FROM
          (SELECT tablename,indexname,indexdef FROM pg_indexes WHERE schemaname='public') x),
        'rls',(SELECT json_agg(x ORDER BY x.relname) FROM
          (SELECT c.relname,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c
             JOIN pg_namespace n ON n.oid=c.relnamespace
             WHERE n.nspname='public' AND c.relkind IN ('r','p')) x),
        'triggers',(SELECT json_agg(x ORDER BY x.relname,x.tgname) FROM
          (SELECT c.relname,t.tgname,pg_get_triggerdef(t.oid) AS definition
             FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
             JOIN pg_namespace n ON n.oid=c.relnamespace
             WHERE n.nspname='public' AND NOT t.tgisinternal) x))::text
    """)
    return digest(cursor.fetchone()[0].encode())


def run():
    import psycopg2
    from psycopg2 import sql
    phase = "configuration"
    owner = None
    try:
        refuse(os.environ.get("ENVIRONMENT") == "staging")
        for name in ("EMAIL_ENABLED", "AUTO_MIGRATE", "JANUA_APPLY_MIGRATIONS", "SESSION_BRIDGE_ENABLED"):
            refuse(os.environ.get(name) == "false")
        refuse(os.environ.get("DATABASE_SSL_MODE") == "verify-full")
        ca = os.environ["DATABASE_SSL_CA_FILE"]
        refuse(ca == "/var/run/janua-staging/ca/ca.crt")
        source = os.environ["SOURCE_SHA"]
        refuse(re.fullmatch(r"[a-f0-9]{40}", source))
        expected = json.loads(os.environ["JANUA_STAGE_MIGRATIONS"])
        grants = json.loads(os.environ["JANUA_STAGE_GRANTS"])
        refuse(expected["source_revision"] == grants["source_revision"] == "73ae24ab4e0cfbd0d09f94c2bfb5e7935d606386")
        refuse(grants["database"] == "janua_staging" and grants["owner_role"] == "enclii_migrator" and grants["runtime_role"] == "janua_staging_runtime")
        refuse(expected["head"] == grants["head"])
        cluster = os.environ["CLUSTER_UID"]
        refuse(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", cluster))
        # The source-selected manifest never comes from an operation request.
        names = [row["name"] for row in grants["tables"]]
        refuse(names == sorted(set(names)) and all(re.fullmatch(r"[a-z][a-z0-9_]{0,62}", n) for n in names))
        graph_bytes = os.environ["JANUA_STAGE_MIGRATIONS"].encode()
        grant_bytes = os.environ["JANUA_STAGE_GRANTS"].encode()
        graph_sha, grants_sha = digest(graph_bytes), digest(grant_bytes)
        migrations = expected["migrations"]
        for row in migrations:
            path = Path("/app") / row["path"].removeprefix("apps/api/")
            refuse(path.parent == Path("/app/alembic/versions"))
            refuse(digest(path.read_bytes()) == row["checksum"])
        # No startup/lifespan/seed hook is invoked. Settings need an ephemeral
        # internal validation key, not a production session/signing credential.
        os.environ["SECRET_KEY"] = secrets.token_urlsafe(48)
        os.environ["DIRECT_DATABASE_URL"] = os.environ["DATABASE_URL"]
        phase = "database_identity"
        owner = psycopg2.connect(os.environ["DATABASE_URL"], sslmode="verify-full", sslrootcert=ca, connect_timeout=5, options="-c statement_timeout=120000 -c lock_timeout=5000")
        owner.autocommit = True
        cur = owner.cursor()
        cur.execute("SELECT pg_try_advisory_lock(1792966561)")
        refuse(cur.fetchone() == (True,))
        cur.execute("SELECT current_database(),current_user,r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication,d.datdba=r.oid,EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid) FROM pg_roles r JOIN pg_database d ON d.datname=current_database() WHERE r.rolname=current_user")
        refuse(cur.fetchone() == ("janua_staging", "enclii_migrator", False, True, False))
        cur.execute("SELECT rolcanlogin,rolsuper,rolbypassrls,rolcreatedb,rolcreaterole,rolreplication,rolinherit FROM pg_roles WHERE rolname='janua_staging_runtime'")
        refuse(cur.fetchone() == (True, False, False, False, False, False, False))
        cur.execute("SELECT count(*) FROM pg_auth_members WHERE member=(SELECT oid FROM pg_roles WHERE rolname='janua_staging_runtime')")
        refuse(cur.fetchone() == (0,))
        # Initial-only: an uncertain/partial earlier attempt cannot be reset or
        # replayed with another key. Recovery is inspection of its retained Job.
        cur.execute("""
          SELECT
            (SELECT count(*) FROM pg_namespace WHERE nspname NOT IN ('pg_catalog','information_schema','public') AND nspname !~ '^pg_') +
            (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname !~ '^pg_') +
            (SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname !~ '^pg_') +
            (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname !~ '^pg_') +
            (SELECT count(*) FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname !~ '^pg_') +
            (SELECT count(*) FROM pg_default_acl) +
            (SELECT count(*) FROM pg_extension WHERE extname!='plpgsql') +
            (SELECT count(*) FROM pg_publication) +
            (SELECT count(*) FROM pg_largeobject_metadata)
        """)
        refuse(cur.fetchone() == (0,))
        phase = "migration"
        logging.disable(logging.CRITICAL)
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            from alembic import command
            from alembic.config import Config
            from alembic.script import ScriptDirectory
            config = Config("/app/alembic.ini")
            config.attributes["janua_staging_isolated"] = True
            config.set_main_option("script_location", "/app/alembic")
            script = ScriptDirectory.from_config(config)
            refuse(script.get_heads() == [expected["head"]])
            refuse(set(r.revision for r in script.walk_revisions()) == {r["revision"] for r in migrations})
            command.upgrade(config, "head")
        ddl_connection = config.attributes.get("janua_staging_connection_receipt")
        expected_ddl = dict(profile="janua-staging-ddl-connection-v1", database="janua_staging", role="enclii_migrator", postgres_major=18, statement_timeout_ms=120000, lock_timeout_ms=5000, tls=True, owner_nonprivileged=True, owner_without_memberships=True, database_owned=True)
        refuse(isinstance(ddl_connection, dict) and ddl_connection == expected_ddl and all(type(ddl_connection[key]) is type(value) for key,value in expected_ddl.items()))
        cur.execute("SELECT version_num FROM public.alembic_version")
        refuse(cur.fetchall() == [(expected["head"],)])
        cur.execute("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
        refuse([r[0] for r in cur.fetchall()] == sorted(names + ["alembic_version"]))
        cur.execute("SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='S' ORDER BY c.relname")
        refuse([r[0] for r in cur.fetchall()] == grants["sequences"])
        cur.execute("SELECT t.typname FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public' AND t.typtype='e' ORDER BY t.typname")
        refuse([r[0] for r in cur.fetchall()] == grants["types"])
        cur.execute("SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' ORDER BY p.proname")
        refuse([r[0] for r in cur.fetchall()] == ["guard_payment_mail_dispatch"])
        cur.execute("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','S') AND pg_get_userbyid(c.relowner) != 'enclii_migrator'")
        refuse(cur.fetchone() == (0,))
        cur.execute("SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'")
        refuse(cur.fetchone() == (0,))
        phase = "synthetic_counts"
        for name in names:
            cur.execute(sql.SQL("SELECT count(*) FROM public.{}").format(sql.Identifier(name)))
            refuse(cur.fetchone() == ((grants["provider_catalog_rows"] if name == "provider_types" else 0),))
        phase = "grants"
        # Grant changes plus protected provenance commit together. Any migration
        # failure/unknown outcome still leaves the whole operation held.
        owner.autocommit = False
        cur.execute("REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC, janua_staging_runtime")
        cur.execute("REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM PUBLIC, janua_staging_runtime")
        cur.execute("REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC, janua_staging_runtime")
        cur.execute("REVOKE CREATE ON SCHEMA public FROM PUBLIC, janua_staging_runtime")
        cur.execute("REVOKE TEMPORARY ON DATABASE janua_staging FROM PUBLIC, janua_staging_runtime")
        for row in grants["tables"]:
            verbs = row["privileges"]
            refuse(set(verbs) <= {"SELECT", "INSERT", "UPDATE", "DELETE"})
            if verbs:
                cur.execute(sql.SQL("GRANT {} ON TABLE public.{} TO janua_staging_runtime").format(sql.SQL(",".join(verbs)), sql.Identifier(row["name"])))
            columns = row["update_columns"]
            refuse(all(re.fullmatch(r"[a-z][a-z0-9_]{0,62}", c) for c in columns))
            if columns:
                cur.execute(sql.SQL("GRANT UPDATE ({}) ON TABLE public.{} TO janua_staging_runtime").format(sql.SQL(",").join(map(sql.Identifier, columns)), sql.Identifier(row["name"])))
        for name in grants["types"]:
            cur.execute(sql.SQL("REVOKE ALL ON TYPE public.{} FROM PUBLIC").format(sql.Identifier(name)))
            if name in grants["runtime_types"]:
                cur.execute(sql.SQL("GRANT USAGE ON TYPE public.{} TO janua_staging_runtime").format(sql.Identifier(name)))
        refuse(grants["functions"] == grants["sequences"] == [])
        cur.execute("CREATE TABLE public.enclii_janua_stage_provenance(singleton integer PRIMARY KEY CHECK(singleton=1),profile text NOT NULL,cluster_uid text NOT NULL,source_revision text NOT NULL,graph_sha256 text NOT NULL,grants_sha256 text NOT NULL)")
        cur.execute("INSERT INTO public.enclii_janua_stage_provenance VALUES(1,'janua-staging-schema-v1',%s,%s,%s,%s)", (cluster, source, graph_sha, grants_sha))
        owner.commit()
        owner.autocommit = True
        phase = "runtime_verification"
        column_inventory = {}
        for name in names:
            cur.execute("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=%s ORDER BY column_name", (name,))
            column_inventory[name] = [c[0] for c in cur.fetchall()]
        with psycopg2.connect(os.environ["JANUA_STAGE_RUNTIME_DATABASE_URL"], sslmode="verify-full", sslrootcert=ca, connect_timeout=5, options="-c statement_timeout=10000 -c lock_timeout=5000") as runtime:
            with runtime.cursor() as probe:
                probe.execute("SELECT current_database(),current_user")
                refuse(probe.fetchone() == ("janua_staging", "janua_staging_runtime"))
                for row in grants["tables"]:
                    for verb in ("SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"):
                        probe.execute("SELECT has_table_privilege(current_user,%s,%s),has_table_privilege(current_user,%s,%s)", ("public."+row["name"], verb, "public."+row["name"], verb+" WITH GRANT OPTION"))
                        refuse(probe.fetchone() == (verb in row["privileges"], False))
                    for column in column_inventory[row["name"]]:
                        probe.execute("SELECT has_column_privilege(current_user,%s,%s,'UPDATE'),has_column_privilege(current_user,%s,%s,'UPDATE WITH GRANT OPTION')", ("public."+row["name"], column, "public."+row["name"], column))
                        refuse(probe.fetchone() == ("UPDATE" in row["privileges"] or column in row["update_columns"], False))
                    if "SELECT" in row["privileges"]:
                        probe.execute(sql.SQL("SELECT count(*) FROM public.{}").format(sql.Identifier(row["name"])))
                        refuse(probe.fetchone() == (0,))
                for table in grants["deny_tables"]:
                    probe.execute("SELECT has_table_privilege(current_user,%s,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')", ("public."+table,))
                    refuse(probe.fetchone() == (False,))
                probe.execute("SELECT has_schema_privilege(current_user,'public','CREATE'),has_database_privilege(current_user,current_database(),'TEMPORARY')")
                refuse(probe.fetchone() == (False, False))
        schema_sha = catalog(cur)
        print(json.dumps(dict(ddl_connection=ddl_connection, executor_sha256=os.environ["JANUA_STAGE_EXECUTOR_SHA256"], schema="janua-staging-schema-v1", source_revision=source, database="janua_staging", owner_role="enclii_migrator", runtime_role="janua_staging_runtime", graph_sha256=graph_sha, grants_sha256=grants_sha, catalog_sha256=schema_sha, head=expected["head"], applied=len(migrations), pending=0, runtime_verified=True, sensitive_rows=0, provider_catalog_rows=grants["provider_catalog_rows"], lineage="FRESH_SYNTHETIC"), separators=(",", ":")))
    except BaseException as error:
        original = getattr(error,"orig",None)
        code = getattr(error,"pgcode",None) or getattr(error,"sqlstate",None) or getattr(original,"pgcode",None) or getattr(original,"sqlstate",None)
        if isinstance(code,str) and re.fullmatch(r"[A-Z0-9]{5}",code):
            sys.stderr.write("Janua staging SQLSTATE "+code+".\n")
        sys.stderr.write("Janua staging refused ("+phase+").\n")
        return 1
    finally:
        if owner is not None:
            owner.close()
    return 0


if __name__ == "__main__":
    sys.exit(run())
