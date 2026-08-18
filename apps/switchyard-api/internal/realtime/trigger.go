package realtime

import (
	"fmt"
	"strings"
)

// This file owns the SQL that installs and removes the LISTEN/NOTIFY plumbing
// on a tenant's addon database. It is the single most injection-sensitive
// surface in the package: schema and table names arrive from an API caller and
// are interpolated into DDL, where bound parameters are not available. Every
// identifier is therefore validated (ValidateIdentifier) and quoted
// (quoteIdent) before it reaches a statement, and both are unit-tested against
// injection payloads.

// triggerFunctionName is the shared NOTIFY trigger function installed once per
// database. All per-table triggers call it; it reads TG_TABLE_SCHEMA /
// TG_TABLE_NAME / TG_OP at runtime so a single function serves every table.
const triggerFunctionName = "enclii_realtime_notify"

// ValidateIdentifier enforces the subset of Postgres identifiers enclii accepts
// for a realtime-watched schema/table. We deliberately reject anything that is
// not [a-z_][a-z0-9_]* (lowercase, unquoted-safe) rather than try to support
// arbitrary quoted identifiers: the accepted set covers the overwhelming
// majority of real tables and eliminates the quoting-escape attack surface.
//
// Returns a descriptive error naming the offending identifier so the API can
// surface a 400 the caller can act on.
func ValidateIdentifier(kind, ident string) error {
	if ident == "" {
		return fmt.Errorf("%s must not be empty", kind)
	}
	if len(ident) > MaxIdentLen {
		return fmt.Errorf("%s %q exceeds %d bytes", kind, ident, MaxIdentLen)
	}
	for i, r := range ident {
		switch {
		case r >= 'a' && r <= 'z':
		case r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return fmt.Errorf("%s %q contains an invalid character %q (allowed: lowercase letters, digits, underscore; must not start with a digit)", kind, ident, string(r))
		}
	}
	return nil
}

// ValidateTableRef validates both halves of a normalized table reference.
func ValidateTableRef(ref TableRef) error {
	ref = ref.Normalize()
	if err := ValidateIdentifier("schema", ref.Schema); err != nil {
		return err
	}
	return ValidateIdentifier("table", ref.Table)
}

// quoteIdent double-quotes a Postgres identifier, escaping embedded quotes.
// ValidateIdentifier has already rejected quotes, so this is defense in depth:
// even if validation were bypassed, the identifier can only ever be read as a
// single name, never as injected SQL.
func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// triggerName is the deterministic per-table trigger name. Namespaced with a
// fixed prefix so enclii's triggers are visible and removable, and so the
// disable path can target exactly what enable created.
func triggerName(ref TableRef) string {
	ref = ref.Normalize()
	return fmt.Sprintf("enclii_realtime_%s_%s", ref.Schema, ref.Table)
}

// BuildFunctionSQL returns the CREATE OR REPLACE for the shared trigger
// function. Idempotent — safe to run on every enable. The function:
//
//   - builds a JSON envelope {event, schema, table, record, old_record, commit_ts}
//   - measures it and, when it would exceed NotifyPayloadCeiling, rebuilds a
//     truncated envelope carrying only the row's primary-key columns (falling
//     back to no record if the table has no PK), with "truncated": true — so a
//     wide row never fails the writer's transaction on the 8 KB NOTIFY cap
//   - pg_notify's the shared Channel
//
// It is written as one string constant (no interpolation) because it contains
// no caller input — only the fixed function/channel names.
func BuildFunctionSQL() string {
	return `
CREATE OR REPLACE FUNCTION ` + quoteIdent(triggerFunctionName) + `() RETURNS trigger AS $enclii$
DECLARE
  payload   json;
  rec       json;
  oldrec    json;
  keycols   text[];
  keyrec    json;
BEGIN
  IF (TG_OP = 'DELETE') THEN
    rec := row_to_json(OLD);
  ELSE
    rec := row_to_json(NEW);
  END IF;

  IF (TG_OP = 'UPDATE' OR TG_OP = 'DELETE') THEN
    oldrec := row_to_json(OLD);
  END IF;

  payload := json_build_object(
    'event', TG_OP,
    'schema', TG_TABLE_SCHEMA,
    'table', TG_TABLE_NAME,
    'record', rec,
    'old_record', oldrec,
    'commit_ts', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
  );

  -- Postgres caps NOTIFY at 8000 bytes and errors on the writer's txn if
  -- exceeded. When the full envelope is too big, emit only the primary-key
  -- columns and flag truncation so the client re-fetches. octet_length counts
  -- bytes (multibyte-safe), not characters.
  IF octet_length(payload::text) > ` + fmt.Sprintf("%d", NotifyPayloadCeiling) + ` THEN
    SELECT array_agg(a.attname) INTO keycols
    FROM pg_index i
    JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
    WHERE i.indrelid = TG_RELID AND i.indisprimary;

    IF keycols IS NOT NULL THEN
      IF (TG_OP = 'DELETE') THEN
        SELECT json_object_agg(k, v) INTO keyrec
        FROM json_each(row_to_json(OLD)) AS e(k, v)
        WHERE k = ANY(keycols);
      ELSE
        SELECT json_object_agg(k, v) INTO keyrec
        FROM json_each(row_to_json(NEW)) AS e(k, v)
        WHERE k = ANY(keycols);
      END IF;
    END IF;

    payload := json_build_object(
      'event', TG_OP,
      'schema', TG_TABLE_SCHEMA,
      'table', TG_TABLE_NAME,
      'record', keyrec,
      'truncated', true,
      'commit_ts', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    );
  END IF;

  PERFORM pg_notify('` + Channel + `', payload::text);

  IF (TG_OP = 'DELETE') THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$enclii$ LANGUAGE plpgsql;`
}

// BuildEnableTableSQL returns the statements to enable realtime on one table:
// (1) install/refresh the shared function, then (2) drop-if-exists + create the
// per-table AFTER trigger. Returned as a slice so the caller runs them in
// order within one transaction. The ref MUST have passed ValidateTableRef.
func BuildEnableTableSQL(ref TableRef) ([]string, error) {
	ref = ref.Normalize()
	if err := ValidateTableRef(ref); err != nil {
		return nil, err
	}
	qSchema := quoteIdent(ref.Schema)
	qTable := quoteIdent(ref.Table)
	qTrigger := quoteIdent(triggerName(ref))
	qFunc := quoteIdent(triggerFunctionName)

	// #nosec G201 -- DDL cannot bind identifiers via placeholders. Every
	// interpolated value here is a schema/table/trigger/function identifier that
	// has passed ValidateTableRef (strict [a-z_][a-z0-9_]* allow-list, ≤63
	// bytes) and is additionally double-quoted by quoteIdent. No caller-supplied
	// data or value literal reaches the statement text.
	stmts := []string{
		BuildFunctionSQL(),
		// DROP first so re-enabling is idempotent and never errors on a
		// pre-existing trigger from a prior enable.
		fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s.%s;", qTrigger, qSchema, qTable),
		fmt.Sprintf(
			"CREATE TRIGGER %s AFTER INSERT OR UPDATE OR DELETE ON %s.%s "+
				"FOR EACH ROW EXECUTE FUNCTION %s();",
			qTrigger, qSchema, qTable, qFunc,
		),
	}
	return stmts, nil
}

// BuildDisableTableSQL returns the statement to remove the per-table trigger.
// The shared function is intentionally left in place (other tables may still
// use it); it is harmless when unused. The ref MUST have passed
// ValidateTableRef.
func BuildDisableTableSQL(ref TableRef) (string, error) {
	ref = ref.Normalize()
	if err := ValidateTableRef(ref); err != nil {
		return "", err
	}
	// #nosec G201 -- identifiers are validated (ValidateTableRef) and quoted
	// (quoteIdent); DDL cannot use bound placeholders for identifiers. See the
	// equivalent note in BuildEnableTableSQL.
	return fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s.%s;",
		quoteIdent(triggerName(ref)), quoteIdent(ref.Schema), quoteIdent(ref.Table)), nil
}

// BuildListTablesSQL returns a query listing the tables that currently have an
// enclii realtime trigger installed, as (schema, table) rows. Used by the
// "list enabled tables" endpoint. No caller input — safe constant.
func BuildListTablesSQL() string {
	return `
SELECT n.nspname AS schema, c.relname AS "table"
FROM pg_trigger t
JOIN pg_class c ON c.oid = t.tgrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE NOT t.tgisinternal
  AND t.tgname LIKE 'enclii_realtime_%'
ORDER BY 1, 2;`
}
