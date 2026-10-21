-- pgbouncer-auth-query.sql — lets the shared pgbouncer (namespace data) look
-- up passwords with auth_query instead of needing every role in its userlist.
--
-- OWNER-RUN, superuser, in the `postgres` database (pgbouncer's auth_dbname).
-- Run it through scripts/pgbouncer-auth-query.sh migrate, which reads the
-- password from the in-cluster Secret pgbouncer-auth and sets the psql
-- variable `auth_password` without printing it. Idempotent: a re-run converges
-- the role, schema, function and grants, and resets the password to the one in
-- the Secret (which is how a rotation lands in Postgres).
--
-- What it creates:
--   role     pgbouncer_auth          LOGIN, nothing else; the only credential
--                                    pgbouncer keeps in its userlist after the
--                                    move (with pgbouncer_admin).
--   schema   pgbouncer               USAGE for pgbouncer_auth only.
--   function pgbouncer.get_auth(text) SECURITY DEFINER, owned by the superuser
--                                    running this, so it can read pg_shadow;
--                                    EXECUTE for pgbouncer_auth only.
--
-- get_auth never returns a superuser, pgbouncer_auth or pgbouncer_admin, or a
-- role whose VALID UNTIL has passed. A superuser that must be pooled stays in
-- the static userlist.
--
-- Rollback: scripts/sql/pgbouncer-auth-query-rollback.sql, only after the
-- pgbouncer config no longer sets auth_query (see the PR runbook).

\set ON_ERROR_STOP on
\set QUIET on

\if :{?auth_password}
\else
  \echo '[FAIL] psql variable auth_password is not set; run this through scripts/pgbouncer-auth-query.sh migrate'
  SELECT 1 / 0 AS auth_password_missing;
\endif

SELECT current_database() = 'postgres' AS in_auth_db \gset
\if :in_auth_db
\else
  \echo '[FAIL] run this in the postgres database (pgbouncer auth_dbname)'
  SELECT 1 / 0 AS wrong_database;
\endif

SELECT rolsuper AS is_superuser FROM pg_roles WHERE rolname = current_user \gset
\if :is_superuser
\else
  \echo '[FAIL] run this as a superuser: get_auth must be owned by one to read pg_shadow'
  SELECT 1 / 0 AS not_superuser;
\endif

BEGIN;

-- Keep the password out of the server log, even if a statement below fails.
SET LOCAL log_statement = 'none';
SET LOCAL log_min_error_statement = 'panic';
SET LOCAL client_min_messages = warning;

SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pgbouncer_auth') AS need_role \gset
\if :need_role
  CREATE ROLE pgbouncer_auth;
  \echo 'created role pgbouncer_auth'
\else
  \echo 'role pgbouncer_auth exists; converging its attributes and password'
\endif

-- Attributes are restated on every run so a hand edit converges back.
-- CONNECTION LIMIT: pgbouncer opens auth connections in the auth_dbname pool,
-- at most max_db_connections (7) per pooler pod, 2 pods.
\set VERBOSITY terse
ALTER ROLE pgbouncer_auth WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
  NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 16
  PASSWORD :'auth_password';
\set VERBOSITY default
\unset auth_password

CREATE SCHEMA IF NOT EXISTS pgbouncer;
REVOKE ALL ON SCHEMA pgbouncer FROM PUBLIC;
GRANT USAGE ON SCHEMA pgbouncer TO pgbouncer_auth;

CREATE OR REPLACE FUNCTION pgbouncer.get_auth(p_usename text)
RETURNS TABLE (usename name, passwd text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT s.usename, s.passwd
  FROM pg_catalog.pg_shadow AS s
  WHERE s.usename = p_usename
    AND NOT s.usesuper
    AND s.usename NOT IN ('pgbouncer_auth', 'pgbouncer_admin')
    AND (s.valuntil IS NULL OR s.valuntil > now())
$$;

COMMENT ON FUNCTION pgbouncer.get_auth(text) IS
  'pgbouncer auth_query lookup (enclii scripts/sql/pgbouncer-auth-query.sql). Returns the stored verifier for one non-superuser login role. EXECUTE is for pgbouncer_auth only.';

REVOKE ALL ON FUNCTION pgbouncer.get_auth(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pgbouncer.get_auth(text) TO pgbouncer_auth;

COMMIT;

\set QUIET off
\echo 'pgbouncer auth_query objects are in place:'
SELECT r.rolname,
       r.rolcanlogin AS can_login,
       r.rolsuper AS superuser,
       r.rolconnlimit AS conn_limit,
       left(a.rolpassword, 14) = 'SCRAM-SHA-256$' AS password_is_scram,
       p.prosecdef AS security_definer,
       pg_get_userbyid(p.proowner) AS function_owner,
       has_function_privilege('pgbouncer_auth', p.oid, 'EXECUTE') AS auth_can_execute,
       has_function_privilege('public', p.oid, 'EXECUTE') AS public_can_execute
FROM pg_roles AS r
JOIN pg_authid AS a ON a.oid = r.oid
CROSS JOIN pg_proc AS p
WHERE r.rolname = 'pgbouncer_auth'
  AND p.oid = 'pgbouncer.get_auth(text)'::regprocedure;
