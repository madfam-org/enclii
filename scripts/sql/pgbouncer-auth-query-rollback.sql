-- pgbouncer-auth-query-rollback.sql — removes what pgbouncer-auth-query.sql
-- created: the function pgbouncer.get_auth(text), the schema pgbouncer and the
-- role pgbouncer_auth.
--
-- OWNER-RUN, superuser, in the `postgres` database. Run it through
-- scripts/pgbouncer-auth-query.sh rollback.
--
-- ORDER MATTERS. Run this only after the pgbouncer pods no longer use
-- auth_query (the config PR reverted and the rollout finished). While a pooler
-- still has auth_query on, removing these makes every role that is not in the
-- static userlist fail pooled auth, which is where they were before the move.
-- Roles in the static userlist are not affected either way.
--
-- Idempotent: a re-run on a database without these objects changes nothing.
-- It refuses to drop the schema if something other than get_auth lives in it.

\set ON_ERROR_STOP on
\set QUIET on
SET client_min_messages = warning;

SELECT current_database() = 'postgres' AS in_auth_db \gset
\if :in_auth_db
\else
  \echo '[FAIL] run this in the postgres database (pgbouncer auth_dbname)'
  SELECT 1 / 0 AS wrong_database;
\endif

BEGIN;

DROP FUNCTION IF EXISTS pgbouncer.get_auth(text);

-- RESTRICT (the default): fails, and rolls back, if anything else was added
-- to the schema since the migration, instead of dropping it silently.
DROP SCHEMA IF EXISTS pgbouncer;

SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pgbouncer_auth') AS have_role \gset
\if :have_role
  -- Grants the role holds elsewhere (none are made by the migration) and
  -- objects it owns would block DROP ROLE; this names them instead.
  REASSIGN OWNED BY pgbouncer_auth TO CURRENT_USER;
  DROP OWNED BY pgbouncer_auth;
  DROP ROLE pgbouncer_auth;
  \echo 'dropped role pgbouncer_auth'
\else
  \echo 'role pgbouncer_auth is already absent'
\endif

COMMIT;

\set QUIET off
SELECT to_regprocedure('pgbouncer.get_auth(text)') IS NULL AS function_gone,
       to_regnamespace('pgbouncer') IS NULL AS schema_gone,
       NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pgbouncer_auth') AS role_gone;
