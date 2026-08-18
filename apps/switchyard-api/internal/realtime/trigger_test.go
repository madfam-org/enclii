package realtime

import (
	"strings"
	"testing"
)

func TestValidateIdentifier(t *testing.T) {
	cases := []struct {
		name    string
		ident   string
		wantErr bool
	}{
		{"simple lowercase", "orders", false},
		{"with underscore", "order_items", false},
		{"with digits", "table1", false},
		{"leading underscore", "_internal", false},
		{"empty", "", true},
		{"leading digit", "1table", true},
		{"uppercase rejected", "Orders", true},
		{"space", "order items", true},
		{"semicolon injection", "orders; DROP TABLE users", true},
		{"double quote", `orders"`, true},
		{"dash", "order-items", true},
		{"dot", "public.orders", true},
		{"parenthesis", "orders()", true},
		{"comment injection", "orders--", true},
		{"too long", strings.Repeat("a", MaxIdentLen+1), true},
		{"max length ok", strings.Repeat("a", MaxIdentLen), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIdentifier("table", tc.ident)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.ident)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.ident, err)
			}
		})
	}
}

func TestValidateTableRef_DefaultsSchema(t *testing.T) {
	// Empty schema normalizes to "public" and passes.
	if err := ValidateTableRef(TableRef{Table: "orders"}); err != nil {
		t.Fatalf("expected default-schema ref to validate, got %v", err)
	}
	// Bad table fails even with valid default schema.
	if err := ValidateTableRef(TableRef{Table: "bad table"}); err == nil {
		t.Fatal("expected invalid table to fail validation")
	}
}

func TestBuildEnableTableSQL_RejectsInjection(t *testing.T) {
	_, err := BuildEnableTableSQL(TableRef{Schema: "public", Table: "orders; DROP TABLE users;--"})
	if err == nil {
		t.Fatal("expected BuildEnableTableSQL to reject an injection payload in the table name")
	}
}

func TestBuildEnableTableSQL_Shape(t *testing.T) {
	stmts, err := BuildEnableTableSQL(TableRef{Schema: "public", Table: "orders"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements (function, drop, create), got %d: %#v", len(stmts), stmts)
	}

	joined := strings.Join(stmts, "\n")

	// The shared function is (re)created.
	if !strings.Contains(joined, "CREATE OR REPLACE FUNCTION") {
		t.Error("expected function creation in enable SQL")
	}
	if !strings.Contains(joined, `"enclii_realtime_notify"`) {
		t.Error("expected the shared trigger function name, quoted")
	}
	// The channel constant is referenced.
	if !strings.Contains(joined, Channel) {
		t.Errorf("expected NOTIFY channel %q in enable SQL", Channel)
	}
	// DROP-then-CREATE for idempotency, both identifier-quoted.
	if !strings.Contains(joined, `DROP TRIGGER IF EXISTS "enclii_realtime_public_orders" ON "public"."orders"`) {
		t.Errorf("expected quoted drop-if-exists; got:\n%s", joined)
	}
	if !strings.Contains(joined, `CREATE TRIGGER "enclii_realtime_public_orders" AFTER INSERT OR UPDATE OR DELETE ON "public"."orders"`) {
		t.Errorf("expected quoted AFTER trigger create; got:\n%s", joined)
	}
	// The payload-ceiling guard must be present so wide rows can't fail the writer.
	if !strings.Contains(joined, "octet_length") {
		t.Error("expected octet_length payload-size guard in the trigger function")
	}
	if !strings.Contains(joined, "'truncated', true") {
		t.Error("expected truncation flag in the oversized-payload branch")
	}
}

func TestBuildDisableTableSQL(t *testing.T) {
	stmt, err := BuildDisableTableSQL(TableRef{Schema: "public", Table: "orders"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `DROP TRIGGER IF EXISTS "enclii_realtime_public_orders" ON "public"."orders";`
	if stmt != want {
		t.Fatalf("disable SQL mismatch:\n got: %s\nwant: %s", stmt, want)
	}

	// Injection in the ref is rejected.
	if _, err := BuildDisableTableSQL(TableRef{Table: `orders"; DROP`}); err == nil {
		t.Fatal("expected disable SQL to reject injection")
	}
}

func TestBuildDisableTableSQL_CustomSchema(t *testing.T) {
	stmt, err := BuildDisableTableSQL(TableRef{Schema: "billing", Table: "invoices"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stmt, `ON "billing"."invoices"`) {
		t.Fatalf("expected custom schema in disable SQL, got %s", stmt)
	}
	if !strings.Contains(stmt, `"enclii_realtime_billing_invoices"`) {
		t.Fatalf("expected schema-qualified trigger name, got %s", stmt)
	}
}

func TestQuoteIdent_EscapesEmbeddedQuotes(t *testing.T) {
	// Defense in depth: even a quote that slipped past validation is neutralized.
	got := quoteIdent(`a"b`)
	want := `"a""b"`
	if got != want {
		t.Fatalf("quoteIdent mismatch: got %s want %s", got, want)
	}
}

func TestBuildFunctionSQL_NoCallerInput(t *testing.T) {
	// The function body is a constant; two calls must be byte-identical.
	if BuildFunctionSQL() != BuildFunctionSQL() {
		t.Fatal("BuildFunctionSQL must be deterministic")
	}
}
