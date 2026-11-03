"""CI-only native migrated-schema proof; no create_all, HTTP or hosted auth."""
import asyncio
import contextlib
import io
import json
import os
import secrets
import sys
import uuid
from datetime import datetime, timedelta

os.environ["SECRET_KEY"] = secrets.token_urlsafe(48)


def run():
    import psycopg2
    import sqlalchemy as sa
    from app.config import settings
    from app.core.database_ssl import asyncpg_options, sync_postgres_options
    from app.models import (
        ActivityLog,
        AuditLog,
        MagicLink,
        OAuthClient,
        Organization,
        OrganizationMember,
        Session,
        User,
        UserEntitlement,
    )
    from app.models.app_role import OrganizationMemberAppRole
    from app.models.organization_authentication_policy import (
        OrganizationAuthenticationPolicy,
    )
    from app.routers.v1.session_bridge import (
        SILENT_AUTH_SCOPE,
        InvalidBridgeState,
        _pair,
        _source_client,
    )
    from sqlalchemy.ext.asyncio import AsyncSession, create_async_engine
    ca = os.environ["DATABASE_SSL_CA_FILE"]
    def connect(variable):
        return psycopg2.connect(os.environ[variable], sslmode="verify-full", sslrootcert=ca)
    models = (User, Session, Organization, OAuthClient, MagicLink, AuditLog, ActivityLog, OrganizationMember, UserEntitlement, OrganizationMemberAppRole, OrganizationAuthenticationPolicy)
    # Every fixture identity/client is seeded ONLY using the owner login.
    users = [str(uuid.uuid4()), str(uuid.uuid4())]
    clients = ["U-901", "F-901"]
    uris = ["https://source.example.invalid/api/auth/session-bridge", "https://target.example.invalid/callback"]
    links = [secrets.token_urlsafe(32), secrets.token_urlsafe(32)]
    with connect("DATABASE_URL") as owner:
        with owner.cursor() as cur:
            for i, identity in enumerate(users):
                cur.execute("INSERT INTO users(id,email,first_name,status,email_verified,mfa_enabled) VALUES(%s,%s,%s,'ACTIVE',%s,false)", (identity,"u90"+str(i)+"@example.invalid",clients[i],bool(i)))
                cur.execute("INSERT INTO magic_links(id,user_id,email,token,expires_at) VALUES(%s,%s,%s,%s,%s)", (str(uuid.uuid4()),identity,"u90"+str(i)+"@example.invalid",links[i],datetime.utcnow()+timedelta(minutes=5)))
                stored = json.dumps([uris[i]])
                if i:
                    stored = json.dumps(stored)  # Actual historical ORM string storage.
                cur.execute("INSERT INTO oauth_clients(id,created_by,client_id,client_secret_hash,client_secret_prefix,name,redirect_uris,allowed_scopes,grant_types,audience,is_active,is_confidential) VALUES(%s,%s,%s,%s,%s,%s,%s::jsonb,%s::jsonb,%s::jsonb,%s,true,true)", (str(uuid.uuid4()),users[0],clients[i],"invalid-synthetic-no-login-hash","U-901",clients[i],stored,json.dumps(["openid",SILENT_AUTH_SCOPE]),json.dumps(["authorization_code","refresh_token"]),"stage-f901"))
    engine = sa.create_engine(os.environ["JANUA_STAGE_RUNTIME_DATABASE_URL"], connect_args=sync_postgres_options(settings))
    with engine.connect() as conn:
        inspector = sa.inspect(conn)
        for model in models:
            columns = {c["name"]: c for c in inspector.get_columns(model.__tablename__)}
            if any(col.name not in columns for col in model.__table__.columns):
                raise ValueError("migrated model column absent")
            if model is not ActivityLog:  # Append-only: no runtime SELECT.
                conn.execute(sa.select(model).limit(1)).all()
    engine.dispose()
    denials = (
        "CREATE TABLE public.unauthorized_stage_fixture(id integer)",
        "ALTER TABLE public.users ADD COLUMN unauthorized_fixture integer",
        "SELECT * FROM public.enclii_janua_stage_provenance",
        "UPDATE public.alembic_version SET version_num='invalid'",
        "GRANT SELECT ON public.users TO PUBLIC",
        "TRUNCATE public.users CASCADE",
        "SET ROLE enclii_migrator",
        "UPDATE public.users SET is_admin=true",
        "UPDATE public.users SET mfa_enabled=false",
        "UPDATE public.organization_members SET role='owner'",
        "INSERT INTO public.organization_member_app_roles DEFAULT VALUES",
        "UPDATE public.oauth_clients SET allowed_scopes='[]'::jsonb",
        "SELECT * FROM public.oauth_client_secrets",
        "SELECT * FROM public.token_pairs",
        "SELECT * FROM public.system_settings",
        "DELETE FROM public.audit_logs",
        "INSERT INTO public.users(id,email) VALUES(gen_random_uuid(),'f901@example.invalid')",
    )
    with connect("JANUA_STAGE_RUNTIME_DATABASE_URL") as runtime:
        with runtime.cursor() as cur:
            for statement in denials:
                cur.execute("SAVEPOINT denied")
                try:
                    cur.execute(statement)
                except psycopg2.Error as error:
                    if error.pgcode != "42501":
                        raise ValueError("unexpected native refusal category") from None
                else:
                    # PostgreSQL may warn/no-op an unauthorized GRANT instead
                    # of raising: prove the ACL did not gain authority.
                    if statement.startswith("GRANT "):
                        cur.execute("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL aclexplode(c.relacl) a WHERE n.nspname='public' AND c.relname='users' AND a.grantee=0 AND a.privilege_type='SELECT'")
                        if cur.fetchone() != (0,):
                            raise ValueError("runtime granted public authority")
                    else:
                        raise ValueError("runtime escalation accepted")
                finally:
                    cur.execute("ROLLBACK TO SAVEPOINT denied")
            # Native spend, both legacy-unverified and already-verified users;
            # no mail issuance or new runtime identity/client provisioning.
            for token, identity in zip(links, users):
                cur.execute("SELECT id FROM magic_links WHERE token=%s AND used_at IS NULL AND expires_at>now() FOR UPDATE", (token,))
                if len(cur.fetchall()) != 1:
                    raise ValueError("synthetic one-use link absent")
                cur.execute("UPDATE magic_links SET used_at=now() WHERE token=%s AND used_at IS NULL", (token,))
                if cur.rowcount != 1:
                    raise ValueError("native one-use claim absent")
                cur.execute("UPDATE users SET email_verified=true,updated_at=now() WHERE id=%s", (identity,))
                cur.execute("SELECT count(*) FROM magic_links WHERE token=%s AND used_at IS NULL", (token,))
                if cur.fetchone() != (0,):
                    raise ValueError("native link replay permitted")
            cur.execute("UPDATE users SET failed_login_attempts=1,last_failed_login=now(),locked_until=NULL,updated_at=now() WHERE id=%s", (users[0],))
            cur.execute("UPDATE oauth_clients SET last_used_at=now(),updated_at=now() WHERE client_id=%s", (clients[0],))
            session_id = str(uuid.uuid4())
            cur.execute("INSERT INTO sessions(id,user_id,token,refresh_token,expires_at,authentication_methods) VALUES(%s,%s,%s,%s,%s,%s::jsonb)", (session_id,users[0],secrets.token_urlsafe(32),secrets.token_urlsafe(32),datetime.utcnow()+timedelta(minutes=5),json.dumps(["pwd","otp"])))
            cur.execute("UPDATE sessions SET revoked=true,is_active=false,revoked_at=now(),revoked_reason='U-901' WHERE id=%s", (session_id,))
            cur.execute("INSERT INTO activity_logs(id,user_id,action) VALUES(%s,%s,'signin')", (str(uuid.uuid4()),users[0]))
        runtime.rollback()
    url,args = asyncpg_options(os.environ["JANUA_STAGE_RUNTIME_DATABASE_URL"],settings)
    transaction = {"source_client_id":clients[0],"target_client_id":clients[1],"source_audience":"stage-f901","source_uri":uris[0],"authorization":{"response_type":"code","client_id":clients[1],"redirect_uri":uris[1],"scope":"openid","state":"a"*43,"nonce":"b"*43,"code_challenge":"c"*43,"code_challenge_method":"S256","prompt":"none"}}
    async def policies():
        db_engine = create_async_engine(url,connect_args=args)
        try:
            async with AsyncSession(db_engine) as session:
                for i,uri in enumerate(uris):
                    found = await _source_client(uri,session)
                    if found.client_id != clients[i]:
                        raise ValueError("migrated JSONB source binding absent")
                await _pair(transaction,session)
                from app.services.auth_service import AuthService
                await AuthService.create_audit_log(session,uuid.UUID(users[0]),None,"U-901",{})
                await session.flush()
                await session.rollback()
            with connect("DATABASE_URL") as owner:
                with owner.cursor() as cur:
                    cur.execute("UPDATE oauth_clients SET is_active=false WHERE client_id=%s",(clients[1],))
            async with AsyncSession(db_engine) as session:
                try:
                    await _pair(transaction,session)
                except InvalidBridgeState:
                    pass
                else:
                    raise ValueError("changed policy admitted")
        finally:
            await db_engine.dispose()
    asyncio.run(policies())
    return {"proof_completed":True,"models_verified":len(models),"model_reads":len(models)-1,"denied_escalations":len(denials),"rolled_back_runtime_write":True,"migrated_policy_read":True,"migrated_pair_recheck":True,"native_magic_claims":2,"http_browser_delivery_proven":False}


if __name__ == "__main__":
    try:
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            receipt = run()
        print(json.dumps(receipt,separators=(",", ":")))
    except BaseException:
        sys.stderr.write("Janua native migration/runtime proof refused.\n")
        sys.exit(1)
