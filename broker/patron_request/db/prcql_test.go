package pr_db

import (
	"testing"

	"github.com/indexdata/cql-go/cql"
)

func TestHandlePatronRequestsQueryKeepsOwnerRestrictionGrouped(t *testing.T) {
	cql := "cql.allRecords = 1 and (side = lending and supplier_symbol_exact = ISIL:REQ or (side = borrowing and requester_symbol_exact = ISIL:REQ))"

	query, err := handlePatronRequestsQuery(cql, 2)
	if err != nil {
		t.Fatalf("handlePatronRequestsQuery() error = %v", err)
	}

	want := "TRUE AND ((side = $3 AND supplier_symbol = $4) OR (side = $5 AND requester_symbol = $6))"
	if got := query.GetWhereClause(); got != want {
		t.Fatalf("where clause = %q, want %q", got, want)
	}
}

func TestFieldTextArrayContainsGenerate(t *testing.T) {
	f := NewFieldTextArrayContains("bibliographic_item_identifiers(ill_request, 'ISBN')").WithFunction("norm_isxn")

	t.Run("eq uses function wrapper", func(t *testing.T) {
		sc := searchClauseForTest("978-3-16-148410-0", "=")
		gotSQL, gotArgs, err := f.Generate(sc, 3)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		wantSQL := "bibliographic_item_identifiers(ill_request, 'ISBN') @> ARRAY[norm_isxn($3)]::text[]"
		if gotSQL != wantSQL {
			t.Fatalf("sql = %q, want %q", gotSQL, wantSQL)
		}
		if len(gotArgs) != 1 || gotArgs[0] != "978-3-16-148410-0" {
			t.Fatalf("args = %#v, want one raw term arg", gotArgs)
		}
	})

	t.Run("ne uses function wrapper", func(t *testing.T) {
		sc := searchClauseForTest("978-3-16-148410-0", "<>")
		gotSQL, gotArgs, err := f.Generate(sc, 4)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		wantSQL := "NOT (bibliographic_item_identifiers(ill_request, 'ISBN') @> ARRAY[norm_isxn($4)]::text[])"
		if gotSQL != wantSQL {
			t.Fatalf("sql = %q, want %q", gotSQL, wantSQL)
		}
		if len(gotArgs) != 1 || gotArgs[0] != "978-3-16-148410-0" {
			t.Fatalf("args = %#v, want one raw term arg", gotArgs)
		}
	})

	t.Run("empty eq checks empty array", func(t *testing.T) {
		sc := searchClauseForTest("", "=")
		gotSQL, gotArgs, err := f.Generate(sc, 5)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		wantSQL := "cardinality(bibliographic_item_identifiers(ill_request, 'ISBN')) = 0"
		if gotSQL != wantSQL {
			t.Fatalf("sql = %q, want %q", gotSQL, wantSQL)
		}
		if len(gotArgs) != 0 {
			t.Fatalf("args = %#v, want empty args", gotArgs)
		}
	})

	t.Run("empty ne checks non-empty array", func(t *testing.T) {
		sc := searchClauseForTest("", "<>")
		gotSQL, gotArgs, err := f.Generate(sc, 6)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		wantSQL := "cardinality(bibliographic_item_identifiers(ill_request, 'ISBN')) > 0"
		if gotSQL != wantSQL {
			t.Fatalf("sql = %q, want %q", gotSQL, wantSQL)
		}
		if len(gotArgs) != 0 {
			t.Fatalf("args = %#v, want empty args", gotArgs)
		}
	})
}

func TestHandlePatronRequestsQueryIsbnUsesNormIsxn(t *testing.T) {
	cql := `isbn = "978-3-16-148410-0"`

	query, err := handlePatronRequestsQuery(cql, 2)
	if err != nil {
		t.Fatalf("handlePatronRequestsQuery() error = %v", err)
	}

	wantWhere := "bibliographic_item_identifiers(ill_request, 'ISBN') @> ARRAY[norm_isxn($3)]::text[]"
	if got := query.GetWhereClause(); got != wantWhere {
		t.Fatalf("where clause = %q, want %q", got, wantWhere)
	}
}

func searchClauseForTest(term, relation string) cql.SearchClause {
	return cql.SearchClause{Term: term, Relation: cql.Relation(relation)}
}
