package pr_db

import "testing"

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
