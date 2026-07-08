package pr_db

import (
	"context"
	"fmt"
	"strings"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/go-utils/utils"
)

var LANGUAGE = utils.GetEnv("LANGUAGE", "english")

const NumberBaseArgs = 2 // SQLC base query has two args: $1=limit, $2=offset

type FieldAllRecords struct{}

func (f *FieldAllRecords) GetColumn() string  { return "" }
func (f *FieldAllRecords) SetColumn(_ string) {}
func (f *FieldAllRecords) Sort() string       { return "" }
func (f *FieldAllRecords) Generate(_ cql.SearchClause, _ int) (string, []any, error) {
	// Accept standard cql.allRecords = 1 (ignore term/relation).
	return "TRUE", nil, nil
}

type FieldTextArrayContains struct {
	column   string
	function string
}

func NewFieldTextArrayContains(column string) *FieldTextArrayContains {
	return &FieldTextArrayContains{column: column}
}

func (f *FieldTextArrayContains) GetColumn() string {
	return f.column
}

func (f *FieldTextArrayContains) SetColumn(column string) {
	f.column = column
}

func (f *FieldTextArrayContains) Sort() string {
	return f.column
}

func (f *FieldTextArrayContains) WithFunction(function string) *FieldTextArrayContains {
	f.function = function
	return f
}

func (f *FieldTextArrayContains) getQueryTermExpr(queryArgumentIndex int) string {
	if f.function == "" {
		return fmt.Sprintf("$%d", queryArgumentIndex)
	}
	return fmt.Sprintf("%s($%d)", f.function, queryArgumentIndex)
}

func (f *FieldTextArrayContains) Generate(sc cql.SearchClause, queryArgumentIndex int) (string, []any, error) {
	if sc.Term == "" && sc.Relation == cql.EQ {
		return "cardinality(" + f.column + ") = 0", []any{}, nil
	}
	if sc.Term == "" && sc.Relation == cql.NE {
		return "cardinality(" + f.column + ") > 0", []any{}, nil
	}
	queryTermExpr := f.getQueryTermExpr(queryArgumentIndex)

	switch sc.Relation {
	case "==", cql.EXACT, cql.EQ:
		return f.column + " @> ARRAY[" + queryTermExpr + "]::text[]", []any{sc.Term}, nil
	case cql.NE:
		return "NOT (" + f.column + " @> ARRAY[" + queryTermExpr + "]::text[])", []any{sc.Term}, nil
	default:
		return "", nil, fmt.Errorf("unsupported relation %s", sc.Relation)
	}
}

// ParsePatronRequestsCql parses cqlString into a pgcql.Query whose placeholder
// numbering starts at $3, matching the two base SQL arguments (limit and offset)
// used by both ListPatronRequestsCql and GetPatronRequestsFacetsCql.
func ParsePatronRequestsCql(cqlString string) (pgcql.Query, error) {
	def := pgcql.NewPgDefinition()

	fa := &FieldAllRecords{}
	def.AddField("cql.allRecords", fa)

	f := pgcql.NewFieldString().WithExact()
	def.AddField("state", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("side", f)

	f = pgcql.NewFieldString().WithLikeOps().WithLower()
	def.AddField("requester_symbol", f)

	f = pgcql.NewFieldString().WithExact().WithColumn("requester_symbol")
	def.AddField("requester_symbol_exact", f)

	f = pgcql.NewFieldString().WithLikeOps().WithLower()
	def.AddField("supplier_symbol", f)

	f = pgcql.NewFieldString().WithExact().WithColumn("supplier_symbol")
	def.AddField("supplier_symbol_exact", f)

	f = pgcql.NewFieldString().WithLikeOps().WithLower()
	def.AddField("requester_req_id", f)

	f = pgcql.NewFieldString().WithExact().WithColumn("requester_req_id")
	def.AddField("requester_req_id_exact", f)

	fb := pgcql.NewFieldBool()
	def.AddField("needs_attention", fb)

	fb = pgcql.NewFieldBool()
	def.AddField("has_notification", fb)

	fb = pgcql.NewFieldBool()
	def.AddField("has_cost", fb)

	fb = pgcql.NewFieldBool()
	def.AddField("has_unread_notification", fb)

	// presence flag only; note contents are not searchable.
	fb = pgcql.NewFieldBool()
	def.AddField("has_internal_note", fb)

	fb = pgcql.NewFieldBool()
	def.AddField("terminal_state", fb)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("service_type", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("service_level", f)

	nf := pgcql.NewFieldDate()
	def.AddField("created_at", nf)

	nf = pgcql.NewFieldDate()
	def.AddField("updated_at", nf)

	nf = pgcql.NewFieldDate()
	def.AddField("needed_at", nf)

	f = pgcql.NewFieldString().WithFullText(LANGUAGE).WithColumn("ill_request->'bibliographicInfo'->>'title'")
	def.AddField("title", f)

	f = pgcql.NewFieldString().WithFullText(LANGUAGE).WithColumn("ill_request->'bibliographicInfo'->>'author'")
	def.AddField("author", f)

	def.AddField("isbn", NewFieldTextArrayContains("bibliographic_item_identifiers(ill_request, 'ISBN')").WithFunction("norm_isxn"))
	def.AddField("issn", NewFieldTextArrayContains("bibliographic_item_identifiers(ill_request, 'ISSN')").WithFunction("norm_isxn"))

	f = pgcql.NewFieldString().WithLikeOps()
	def.AddField("patron", f)

	f = pgcql.NewFieldString().WithSplit().WithExact()
	def.AddField("id", f)

	f = pgcql.NewFieldString().WithLikeOps().WithLower().WithColumn("ill_request->'patronInfo'->>'givenName'")
	def.AddField("given_name", f)

	f = pgcql.NewFieldString().WithLikeOps().WithLower().WithColumn("ill_request->'patronInfo'->>'surname'")
	def.AddField("surname", f)

	ftv := pgcql.NewFieldTsVector().WithLanguage(LANGUAGE).WithServerChoiceRel(cql.ALL).WithColumn("search")
	def.AddField("cql.serverChoice", ftv)

	f = pgcql.NewFieldString().WithLikeOps().WithLower()
	def.AddField("requester_name", f)

	f = pgcql.NewFieldString().WithLikeOps().WithLower()
	def.AddField("supplier_name", f)

	var parser cql.Parser
	query, err := parser.Parse(cqlString)
	if err != nil {
		return nil, err
	}
	return def.Parse(query, NumberBaseArgs+1)
}

// facetFieldPlaceholder is the column name used in the getPatronRequestsFacets SQL template.
// GetPatronRequestsFacetsCql substitutes it with the validated facet field at runtime.
const facetFieldPlaceholder = "requester_symbol"

func (q *Queries) GetPatronRequestsFacetsCql(ctx context.Context, db DBTX, facetField string, pgcql pgcql.Query) ([]GetPatronRequestsFacetsRow, error) {
	if pgcql == nil {
		return nil, fmt.Errorf("pgcql.Query must not be nil; use cql.allRecords=1 for no filter")
	}
	// facetField is validated against an allowlist by the caller (GetPatronRequestsFacets),
	// so it is safe to substitute directly as a column name.
	sql := strings.Replace(getPatronRequestsFacets, facetFieldPlaceholder, facetField, 1)

	idx := strings.Index(sql, "GROUP BY")
	if idx == -1 {
		return nil, fmt.Errorf("base SQL query missing GROUP BY clause")
	}
	if pgcql.GetWhereClause() != "" {
		sql = sql[:idx] + "AND (" + pgcql.GetWhereClause() + ") " + sql[idx:]
	}
	sqlArguments := make([]interface{}, 0, NumberBaseArgs+len(pgcql.GetQueryArguments()))
	sqlArguments = append(sqlArguments, int64(100), int64(0)) // 100 facet values should be more than enough; offset is always 0 for facets
	sqlArguments = append(sqlArguments, pgcql.GetQueryArguments()...)
	rows, err := db.Query(ctx, sql, sqlArguments...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute facets query: %w", err)
	}
	defer rows.Close()
	var items []GetPatronRequestsFacetsRow
	for rows.Next() {
		var i GetPatronRequestsFacetsRow
		if err := rows.Scan(&i.Value, &i.Count); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (q *Queries) ListPatronRequestsCql(ctx context.Context, db DBTX, arg ListPatronRequestsParams,
	pgcql pgcql.Query, explainAnalyze bool) ([]ListPatronRequestsRow, []string, error) {
	if pgcql == nil {
		return nil, nil, fmt.Errorf("pgcql.Query must not be nil; use cql.allRecords=1 for no filter")
	}
	orgSql := listPatronRequests
	pos := strings.Index(orgSql, "ORDER BY")
	if pos == -1 {
		return nil, nil, fmt.Errorf("CQL query must contain an ORDER BY clause")
	}
	limitPos := strings.Index(orgSql, "LIMIT")
	if limitPos == -1 {
		return nil, nil, fmt.Errorf("base query missing LIMIT")
	}
	orderBy := orgSql[pos:limitPos]
	if pgcql.GetOrderByClause() != "" {
		orderBy = pgcql.GetOrderByClause() + " "
	}
	sqlPrefix := orgSql[:pos]
	if pgcql.GetWhereClause() != "" {
		if strings.Contains(strings.ToUpper(sqlPrefix), "WHERE ") {
			sqlPrefix += "AND " + pgcql.GetWhereClause() + " "
		} else {
			sqlPrefix += "WHERE " + pgcql.GetWhereClause() + " "
		}
	}
	sql := sqlPrefix + orderBy + orgSql[limitPos:]
	sqlArguments := make([]interface{}, 0, NumberBaseArgs+len(pgcql.GetQueryArguments()))
	sqlArguments = append(sqlArguments, arg.Limit, arg.Offset)
	sqlArguments = append(sqlArguments, pgcql.GetQueryArguments()...)
	explainResult := []string{}
	if explainAnalyze {
		explainRows, err := db.Query(ctx, "EXPLAIN ANALYZE "+sql, sqlArguments...)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to run query with EXPLAIN ANALYZE: %w", err)
		}
		defer explainRows.Close()
		for explainRows.Next() {
			var line string
			if err := explainRows.Scan(&line); err != nil {
				return nil, nil, fmt.Errorf("failed to read EXPLAIN ANALYZE output: %w", err)
			}
			explainResult = append(explainResult, line)
		}
		if err := explainRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("error reading EXPLAIN ANALYZE output: %w", err)
		}
	}
	rows, err := db.Query(ctx, sql, sqlArguments...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert CQL to SQL: %w", err)
	}
	defer rows.Close()
	var items []ListPatronRequestsRow
	for rows.Next() {
		var i ListPatronRequestsRow
		if err := rows.Scan(
			&i.PatronRequestSearchView.ID,
			&i.PatronRequestSearchView.CreatedAt,
			&i.PatronRequestSearchView.IllRequest,
			&i.PatronRequestSearchView.State,
			&i.PatronRequestSearchView.Side,
			&i.PatronRequestSearchView.Patron,
			&i.PatronRequestSearchView.RequesterSymbol,
			&i.PatronRequestSearchView.SupplierSymbol,
			&i.PatronRequestSearchView.Tenant,
			&i.PatronRequestSearchView.RequesterReqID,
			&i.PatronRequestSearchView.NeedsAttention,
			&i.PatronRequestSearchView.LastAction,
			&i.PatronRequestSearchView.LastActionOutcome,
			&i.PatronRequestSearchView.LastActionResult,
			&i.PatronRequestSearchView.Items,
			&i.PatronRequestSearchView.Language,
			&i.PatronRequestSearchView.TerminalState,
			&i.PatronRequestSearchView.UpdatedAt,
			&i.PatronRequestSearchView.IllResponse,
			&i.PatronRequestSearchView.InternalNote,
			&i.PatronRequestSearchView.NextReqID,
			&i.PatronRequestSearchView.PrevReqID,
			&i.PatronRequestSearchView.RetryBibInfo,
			&i.PatronRequestSearchView.HasNotification,
			&i.PatronRequestSearchView.HasCost,
			&i.PatronRequestSearchView.HasUnreadNotification,
			&i.PatronRequestSearchView.HasInternalNote,
			&i.PatronRequestSearchView.ServiceType,
			&i.PatronRequestSearchView.ServiceLevel,
			&i.PatronRequestSearchView.NeededAt,
			&i.PatronRequestSearchView.UnreadNotificationsCount,
			&i.PatronRequestSearchView.RequesterName,
			&i.PatronRequestSearchView.SupplierName,
			&i.FullCount,
		); err != nil {
			return nil, nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return items, explainResult, nil
}
