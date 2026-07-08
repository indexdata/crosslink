package pr_db

import (
	"testing"

	"github.com/indexdata/cql-go/cql"
	"github.com/stretchr/testify/assert"
)

func TestHandlePatronRequestsQueryKeepsOwnerRestrictionGrouped(t *testing.T) {
	cql := "cql.allRecords = 1 and (side = lending and supplier_symbol_exact = ISIL:REQ or (side = borrowing and requester_symbol_exact = ISIL:REQ))"

	query, err := ParsePatronRequestsCql(cql)
	assert.NoError(t, err, "ParsePatronRequestsCQL() error = %v", err)

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

	query, err := ParsePatronRequestsCql(cql)
	assert.NoError(t, err, "ParsePatronRequestsCQL() error = %v", err)

	wantWhere := "bibliographic_item_identifiers(ill_request, 'ISBN') @> ARRAY[norm_isxn($3)]::text[]"
	assert.Equal(t, wantWhere, query.GetWhereClause(), "where clause = %q, want %q", query.GetWhereClause(), wantWhere)
}

func searchClauseForTest(term, relation string) cql.SearchClause {
	return cql.SearchClause{Term: term, Relation: cql.Relation(relation)}
}

func TestFieldPeerNameGenerate(t *testing.T) {
	f := NewFieldPeerName("requester_symbol")

	tests := []struct {
		name      string
		term      string
		relation  string
		wantSQL   string
		wantArg   string
		wantError bool
	}{
		{
			name:     "exact match uses IN + =",
			term:     "ISIL:REQ-1",
			relation: "=",
			wantSQL:  "requester_symbol IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) = lower($3))",
			wantArg:  "ISIL:REQ-1",
		},
		{
			name:     "lowercase term uses IN + =",
			term:     "isil:req-1",
			relation: "=",
			wantSQL:  "requester_symbol IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) = lower($3))",
			wantArg:  "isil:req-1",
		},
		{
			name:     "prefix wildcard uses IN + LIKE",
			term:     "ISIL:REQ*",
			relation: "=",
			wantSQL:  "requester_symbol IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) LIKE lower($3))",
			wantArg:  "ISIL:REQ%",
		},
		{
			name:     "leading wildcard uses IN + LIKE",
			term:     "*REQ-1",
			relation: "=",
			wantSQL:  "requester_symbol IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) LIKE lower($3))",
			wantArg:  "%REQ-1",
		},
		{
			name:     "ne exact match uses NOT IN + =",
			term:     "ISIL:REQ-1",
			relation: "<>",
			wantSQL:  "requester_symbol NOT IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) = lower($3))",
			wantArg:  "ISIL:REQ-1",
		},
		{
			name:     "ne wildcard uses NOT IN + LIKE",
			term:     "ISIL:REQ*",
			relation: "<>",
			wantSQL:  "requester_symbol NOT IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) LIKE lower($3))",
			wantArg:  "ISIL:REQ%",
		},
		{
			name:      "unsupported relation returns error",
			term:      "foo",
			relation:  "adj",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := searchClauseForTest(tt.term, tt.relation)
			gotSQL, gotArgs, err := f.Generate(sc, 3)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got sql=%q", gotSQL)
				}
				return
			}
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Fatalf("sql:\n  got  %q\n  want %q", gotSQL, tt.wantSQL)
			}
			if len(gotArgs) != 1 || gotArgs[0] != tt.wantArg {
				t.Fatalf("args = %#v, want [%q]", gotArgs, tt.wantArg)
			}
		})
	}
}

func TestParseRequesterSupplierNameCQL(t *testing.T) {
	tests := []struct {
		cql       string
		wantWhere string
	}{
		{
			cql:       "requester_name = ISIL:REQ-1",
			wantWhere: "requester_symbol IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) = lower($3))",
		},
		{
			cql:       "supplier_name = ISIL:SUP*",
			wantWhere: "supplier_symbol IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) LIKE lower($3))",
		},
		{
			cql:       "requester_name <> ISIL:REQ-1",
			wantWhere: "requester_symbol NOT IN (SELECT s.symbol_value FROM peer p JOIN symbol s ON s.peer_id = p.id WHERE lower(p.name) = lower($3))",
		},
	}
	for _, tt := range tests {
		t.Run(tt.cql, func(t *testing.T) {
			q, err := ParsePatronRequestsCql(tt.cql)
			if err != nil {
				t.Fatalf("ParsePatronRequestsCql(%q) error = %v", tt.cql, err)
			}
			if got := q.GetWhereClause(); got != tt.wantWhere {
				t.Fatalf("where clause:\n  got  %q\n  want %q", got, tt.wantWhere)
			}
		})
	}
}
