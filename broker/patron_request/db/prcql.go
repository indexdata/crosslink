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

type FieldAllRecords struct{}

func (f *FieldAllRecords) GetColumn() string       { return "" }
func (f *FieldAllRecords) SetColumn(column string) {}
func (f *FieldAllRecords) Sort() string            { return "" }
func (f *FieldAllRecords) Generate(sc cql.SearchClause, queryArgumentIndex int) (string, []any, error) {
	// Accept standard cql.allRecords = 1 (ignore term/relation).
	return "TRUE", nil, nil
}

type FieldTextArrayContains struct {
	column string
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

func (f *FieldTextArrayContains) Generate(sc cql.SearchClause, queryArgumentIndex int) (string, []any, error) {
	if sc.Term == "" && sc.Relation == cql.EQ {
		return "cardinality(" + f.column + ") > 0", []any{}, nil
	}

	switch sc.Relation {
	case "==", cql.EXACT, cql.EQ:
		return f.column + fmt.Sprintf(" @> ARRAY[$%d]::text[]", queryArgumentIndex), []any{sc.Term}, nil
	case cql.NE:
		return "NOT (" + f.column + fmt.Sprintf(" @> ARRAY[$%d]::text[]", queryArgumentIndex) + ")", []any{sc.Term}, nil
	default:
		return "", nil, fmt.Errorf("unsupported relation %s", sc.Relation)
	}
}

func handlePatronRequestsQuery(cqlString string, noBaseArgs int) (pgcql.Query, error) {
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

	def.AddField("isbn", NewFieldTextArrayContains("bibliographic_item_identifiers(ill_request, 'ISBN')"))
	def.AddField("issn", NewFieldTextArrayContains("bibliographic_item_identifiers(ill_request, 'ISSN')"))

	f = pgcql.NewFieldString().WithLikeOps()
	def.AddField("patron", f)

	f = pgcql.NewFieldString().WithSplit().WithExact()
	def.AddField("id", f)

	ftv := pgcql.NewFieldTsVector().WithLanguage(LANGUAGE).WithServerChoiceRel(cql.ALL).WithColumn("search")
	def.AddField("cql.serverChoice", ftv)

	var parser cql.Parser
	query, err := parser.Parse(cqlString)
	if err != nil {
		return nil, err
	}
	return def.Parse(query, noBaseArgs+1)
}

func (q *Queries) ListPatronRequestsCql(ctx context.Context, db DBTX, arg ListPatronRequestsParams,
	cqlString *string, explainAnalyze bool) ([]ListPatronRequestsRow, []string, error) {
	if cqlString == nil {
		rows, err := q.ListPatronRequests(ctx, db, arg)
		return rows, nil, err
	}
	noBaseArgs := 2 // we have two base arguments: limit and offset
	res, err := handlePatronRequestsQuery(*cqlString, noBaseArgs)
	if err != nil {
		return nil, nil, err
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
	if res.GetOrderByClause() != "" {
		orderBy = res.GetOrderByClause() + " "
	}
	sqlPrefix := orgSql[:pos]
	if res.GetWhereClause() != "" {
		if strings.Contains(strings.ToUpper(sqlPrefix), "WHERE ") {
			sqlPrefix += "AND " + res.GetWhereClause() + " "
		} else {
			sqlPrefix += "WHERE " + res.GetWhereClause() + " "
		}
	}
	sql := sqlPrefix + orderBy + orgSql[limitPos:]
	sqlArguments := make([]interface{}, 0, noBaseArgs+len(res.GetQueryArguments()))
	sqlArguments = append(sqlArguments, arg.Limit, arg.Offset)
	sqlArguments = append(sqlArguments, res.GetQueryArguments()...)
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
			&i.PatronRequestSearchView.HasNotification,
			&i.PatronRequestSearchView.HasCost,
			&i.PatronRequestSearchView.HasUnreadNotification,
			&i.PatronRequestSearchView.ServiceType,
			&i.PatronRequestSearchView.ServiceLevel,
			&i.PatronRequestSearchView.NeededAt,
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
