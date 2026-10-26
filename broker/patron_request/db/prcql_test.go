package pr_db

import (
	"fmt"
	"testing"
	"time"

	"github.com/indexdata/cql-go/cql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatronRequestDateWithin(t *testing.T) {
	for _, field := range []string{"created_at", "updated_at", "needed_at", "due_at"} {
		t.Run(field, func(t *testing.T) {
			for _, tc := range []struct {
				name, lower, upper, upperRelation string
				wantLower, wantUpper              time.Time
			}{
				{"date range", "2026-09-01", "2026-09-17", "<",
					time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)},
				{"same day", "2026-09-17", "2026-09-17", "<",
					time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)},
				{"month end", "2026-09-01", "2026-09-30", "<",
					time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
				{"explicit midnight", "2026-09-01", "2026-09-17T00:00:00Z", "<=",
					time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)},
				{"explicit timestamps", "2026-09-01T09:00:00Z", "2026-09-17T15:30:00Z", "<=",
					time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, 9, 17, 15, 30, 0, 0, time.UTC)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					query, err := ParsePatronRequestsCql(fmt.Sprintf(`%s within "%s %s" and side = borrowing`, field, tc.lower, tc.upper))
					require.NoError(t, err)
					assert.Equal(t, fmt.Sprintf("(%s >= $3 AND %s %s $4) AND side = $5", field, field, tc.upperRelation), query.GetWhereClause())
					assert.Equal(t, []any{tc.wantLower, tc.wantUpper, "borrowing"}, query.GetQueryArguments())
				})
			}
		})
	}
}

func TestHandlePatronRequestsQueryKeepsOwnerRestrictionGrouped(t *testing.T) {
	cql := "cql.allRecords = 1 and (side = lending and supplier_symbol_exact = ISIL:REQ or (side = borrowing and requester_symbol_exact = ISIL:REQ))"

	query, err := ParsePatronRequestsCql(cql)
	assert.NoError(t, err, "ParsePatronRequestsCQL() error = %v", err)

	want := "TRUE AND ((side = $3 AND supplier_symbol = $4) OR (side = $5 AND requester_symbol = $6))"
	if got := query.GetWhereClause(); got != want {
		t.Fatalf("where clause = %q, want %q", got, want)
	}
}

func TestDefaultAgingCqlIncludesMissingServiceLevel(t *testing.T) {
	cql := `side = lending and (state = VALIDATED or state = ITEM_PENDING or state = WILL_SUPPLY_PENDING or state = WILL_SUPPLY or state = CONDITION_PENDING or state = SEARCHING) and ((cql.allRecords = 1 not service_level = "") or (service_level <> Rush and service_level <> Express))`

	query, err := ParsePatronRequestsCql(cql)
	require.NoError(t, err)
	assert.Contains(
		t,
		query.GetWhereClause(),
		`(TRUE AND NOT service_level IS NOT NULL) OR (service_level <> $10 AND service_level <> $11)`,
	)
	assert.Equal(t, []any{
		"lending",
		"VALIDATED",
		"ITEM_PENDING",
		"WILL_SUPPLY_PENDING",
		"WILL_SUPPLY",
		"CONDITION_PENDING",
		"SEARCHING",
		"Rush",
		"Express",
	}, query.GetQueryArguments())
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

func TestPatronRequestDuplicateFields(t *testing.T) {
	query, err := ParsePatronRequestsCql(`patron_exact = "patron-1" and supplier_unique_record_id = "record-1" and title_exact = "A Title" and id <> "current-pr"`)
	assert.NoError(t, err)
	assert.Equal(t, "((patron = $3 AND ill_request->'bibliographicInfo'->>'supplierUniqueRecordId' = $4) AND lower(ill_request->'bibliographicInfo'->>'title') = lower($5)) AND id NOT IN($6)", query.GetWhereClause())
	assert.Equal(t, []any{"patron-1", "record-1", "A Title", "current-pr"}, query.GetQueryArguments())
}

func TestHandlePatronRequestsQueryItemFieldsUseExists(t *testing.T) {
	tests := []struct {
		name      string
		cql       string
		wantWhere string
	}{
		{
			name:      "item_id exact",
			cql:       `item_id = "item-123"`,
			wantWhere: "EXISTS (SELECT 1 FROM item cql_item WHERE cql_item.pr_id = patron_request_search_view.id AND lower(cql_item.item_id) = lower($3))",
		},
		{
			name:      "barcode wildcard",
			cql:       `barcode = "abc*"`,
			wantWhere: "EXISTS (SELECT 1 FROM item cql_item WHERE cql_item.pr_id = patron_request_search_view.id AND lower(cql_item.barcode) LIKE lower($3))",
		},
		{
			name:      "call_number empty",
			cql:       `call_number = ""`,
			wantWhere: "NOT EXISTS (SELECT 1 FROM item cql_item WHERE cql_item.pr_id = patron_request_search_view.id AND cql_item.call_number IS NOT NULL AND cql_item.call_number <> '')",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query, err := ParsePatronRequestsCql(tc.cql)
			if err != nil {
				t.Fatalf("ParsePatronRequestsCql() error = %v", err)
			}
			if got := query.GetWhereClause(); got != tc.wantWhere {
				t.Fatalf("where clause = %q, want %q", got, tc.wantWhere)
			}
		})
	}
}

func searchClauseForTest(term, relation string) cql.SearchClause {
	return cql.SearchClause{Term: term, Relation: cql.Relation(relation)}
}
