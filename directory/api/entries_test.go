package api

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildEntrySQL(t *testing.T) {
	// Test with no WHERE clause
	sql := buildEntrySQL("")
	if !strings.Contains(sql, "SELECT") {
		t.Error("SQL should contain SELECT")
	}
	if !strings.Contains(sql, "FROM entries e") {
		t.Error("SQL should contain FROM entries e")
	}

	// Test with WHERE clause
	sql = buildEntrySQL("WHERE e.id = $1")
	if !strings.Contains(sql, "WHERE e.id = $1") {
		t.Error("SQL should contain WHERE clause")
	}

	// Test with ORDER BY clause
	sql = buildEntrySQL("ORDER BY e.name")
	if !strings.Contains(sql, "ORDER BY e.name") {
		t.Error("SQL should contain ORDER BY clause")
	}
}

func TestHandleEntryCQL(t *testing.T) {
	// Test simple name search
	res, err := handleEntryCQL("name=foo", 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.GetWhereClause() == "" {
		t.Error("Expected WHERE clause, got empty string")
	}
	args := res.GetQueryArguments()
	if len(args) != 1 {
		t.Errorf("Expected 1 argument, got %d", len(args))
	}

	// Test description search
	res, err = handleEntryCQL("description=library", 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.GetWhereClause() == "" {
		t.Error("Expected WHERE clause, got empty string")
	}

	// Test wildcard search
	res, err = handleEntryCQL("name=*foo*", 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.GetWhereClause() == "" {
		t.Error("Expected WHERE clause, got empty string")
	}
	if !strings.Contains(res.GetWhereClause(), "LIKE") {
		t.Error("Expected LIKE operator for wildcard search")
	}

	// Test combined query
	res, err = handleEntryCQL("name=foo AND description=bar", 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if res.GetWhereClause() == "" {
		t.Error("Expected WHERE clause, got empty string")
	}
	args = res.GetQueryArguments()
	if len(args) != 2 {
		t.Errorf("Expected 2 arguments, got %d", len(args))
	}

	// Test parent symbol search
	res, err = handleEntryCQL(`parentSymbol any "ISIL:PARENT PARENT2"`, 0)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !strings.Contains(res.GetWhereClause(), "entry_symbol.owner = e.parent") {
		t.Errorf("Expected parentSymbol to match symbols owned by the parent: %s", res.GetWhereClause())
	}
	args = res.GetQueryArguments()
	if len(args) != 1 {
		t.Errorf("Expected 1 parentSymbol argument, got %d", len(args))
	}

	// Reject the removed LMS-code filter and invalid boolean terms.
	for _, query := range []string{`requesterPickupLocation <> ""`, `isPickupLocation=invalid`} {
		if _, err := handleEntryCQL(query, 0); err == nil {
			t.Errorf("Expected error for %s", query)
		}
	}

	// Test invalid CQL
	_, err = handleEntryCQL("invalid cql query (((", 0)
	if err == nil {
		t.Error("Expected error for invalid CQL, got nil")
	}
}

func TestHandleEntryCQLCaseInsensitive(t *testing.T) {
	for _, field := range []string{"name", "description"} {
		t.Run(field, func(t *testing.T) {
			for _, tc := range []struct {
				clause string
				op     string
				term   string
			}{
				{`="Central Library"`, "=", "Central Library"},
				{`=="Central Library"`, "=", "Central Library"},
				{` exact "Central Library"`, "=", "Central Library"},
				{`<>"Central Library"`, "<>", "Central Library"},
				{`="*Central?Library*"`, "LIKE", "%Central_Library%"},
				{`<>"*Central*"`, "NOT LIKE", "%Central%"},
				{`="Central\*Library"`, "=", "Central*Library"},
				{`="*A\"B\\C*"`, "LIKE", "%A\"B\\\\C%"},
				{`="*Central_100%*"`, "LIKE", "%Central\\_100\\%%"},
			} {
				t.Run(tc.clause, func(t *testing.T) {
					res, err := handleEntryCQL(field+tc.clause, 0)
					if err != nil {
						t.Fatalf("translate CQL: %v", err)
					}
					want := "lower(e." + field + ") " + tc.op + " lower($1)"
					if got := res.GetWhereClause(); got != want {
						t.Errorf("WHERE clause = %q, want %q", got, want)
					}
					if got := res.GetQueryArguments(); !reflect.DeepEqual(got, []any{tc.term}) {
						t.Errorf("arguments = %#v, want %#v", got, []any{tc.term})
					}
				})
			}
		})
	}

	t.Run("combined fields with existing arguments", func(t *testing.T) {
		res, err := handleEntryCQL(`name="*Central*" OR description="*Library*"`, 1)
		if err != nil {
			t.Fatalf("translate combined CQL: %v", err)
		}
		want := "lower(e.name) LIKE lower($2) OR lower(e.description) LIKE lower($3)"
		if got := res.GetWhereClause(); got != want {
			t.Errorf("WHERE clause = %q, want %q", got, want)
		}
		if got := res.GetQueryArguments(); !reflect.DeepEqual(got, []any{"%Central%", "%Library%"}) {
			t.Errorf("unexpected arguments: %#v", got)
		}
	})

	t.Run("type remains case sensitive", func(t *testing.T) {
		res, err := handleEntryCQL(`type="*Institution*"`, 0)
		if err != nil {
			t.Fatalf("translate type CQL: %v", err)
		}
		if got := res.GetWhereClause(); got != "e.type LIKE $1" {
			t.Errorf("unexpected type predicate: %s", got)
		}
	})
}

func TestHandleEntryCQLServerChoice(t *testing.T) {
	for _, tc := range []struct {
		query string
		term  string
	}{
		{`cql.serverChoice="Central Library"`, "'Central'&'Library'"},
		{`"Central Library"`, "'Central'&'Library'"},
		{`cql.serverChoice all "Central Library"`, "'Central'&'Library'"},
		{`cql.serverChoice any "Central Library"`, "'Central'|'Library'"},
		{`cql.serverChoice adj "Central Library"`, "'Central'<->'Library'"},
		{`cql.serverChoice="Centr*"`, "'Centr':*"},
		{`cql.serverChoice="O'Brien"`, "'O''Brien'"},
		{`cql.serverChoice="A\"B\\C"`, "'A\"B\\C'"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			res, err := handleEntryCQL(tc.query, 0)
			if err != nil {
				t.Fatalf("translate serverChoice CQL: %v", err)
			}
			if got := res.GetWhereClause(); got != "e.search @@ to_tsquery('simple', $1)" {
				t.Errorf("unexpected search predicate: %s", got)
			}
			if got := res.GetQueryArguments(); !reflect.DeepEqual(got, []any{tc.term}) {
				t.Errorf("arguments = %#v, want %#v", got, []any{tc.term})
			}
		})
	}
	t.Run("composes with existing filters and offsets", func(t *testing.T) {
		res, err := handleEntryCQL(`cql.serverChoice="Central" AND isPickupLocation=true`, 1)
		if err != nil {
			t.Fatalf("translate combined CQL: %v", err)
		}
		if got := res.GetWhereClause(); !strings.HasPrefix(got, "e.search @@ to_tsquery('simple', $2) AND ") || !strings.HasSuffix(got, " = $3") {
			t.Errorf("unexpected combined predicate: %s", got)
		}
		if got := res.GetQueryArguments(); !reflect.DeepEqual(got, []any{"'Central'", true}) {
			t.Errorf("unexpected combined arguments: %#v", got)
		}
	})
	for _, query := range []string{`cql.serverChoice="*central*"`, `cql.serverChoice="centr?"`} {
		if _, err := handleEntryCQL(query, 0); err == nil {
			t.Errorf("expected unsupported wildcard error for %s", query)
		}
	}
}

func TestBuildOwnedEntryListQueryScopesCQL(t *testing.T) {
	cqlQuery := `name="Owned" OR tenant="OTHER"`
	limit := Limit(5)
	offset := Offset(2)
	query, args, err := buildEntryListQuery(
		&cqlQuery,
		&limit,
		&offset,
		"e.tenant = $1",
		[]any{"ANINST"},
	)
	if err != nil {
		t.Fatalf("building owned entry query: %v", err)
	}

	if !strings.Contains(query, "WHERE e.tenant = $1 AND (") {
		t.Fatalf("CQL predicate is not grouped within tenant scope: %s", query)
	}
	if !strings.Contains(query, "LIMIT $4") || !strings.Contains(query, "OFFSET $5") {
		t.Fatalf("query arguments were not numbered after tenant and CQL arguments: %s", query)
	}
	wantArgs := []any{"ANINST", "Owned", "OTHER", 5, Offset(2)}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected query arguments: got %#v, want %#v", args, wantArgs)
	}
}
