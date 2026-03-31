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

func handlePatronRequestsQuery(cqlString string, noBaseArgs int) (pgcql.Query, error) {
	def := pgcql.NewPgDefinition()

	fa := &FieldAllRecords{}
	def.AddField("cql.allRecords", fa)

	f := pgcql.NewFieldString().WithExact()
	def.AddField("state", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("side", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("requester_symbol", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("supplier_symbol", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("requester_req_id", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("needs_attention", f)

	fb := pgcql.NewFieldBool()
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

	nf := pgcql.NewFieldDate().WithColumn("timestamp")
	def.AddField("created_at", nf)

	nf = pgcql.NewFieldDate()
	def.AddField("needed_at", nf)

	f = pgcql.NewFieldString().WithExact().WithColumn("ill_request->'bibliographicInfo'->>'title'")
	def.AddField("title", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("patron", f)

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
	cqlString *string) ([]ListPatronRequestsRow, error) {
	if cqlString == nil {
		return q.ListPatronRequests(ctx, db, arg)
	}
	noBaseArgs := 2 // we have two base arguments: limit and offset
	res, err := handlePatronRequestsQuery(*cqlString, noBaseArgs)
	if err != nil {
		return nil, err
	}
	whereClause := ""
	if res.GetWhereClause() != "" {
		whereClause = "WHERE " + res.GetWhereClause() + " "
	}
	orgSql := listPatronRequests
	pos := strings.Index(orgSql, "ORDER BY")
	if pos == -1 {
		return nil, fmt.Errorf("CQL query must contain an ORDER BY clause")
	}
	sql := orgSql[:pos] + whereClause + orgSql[pos:]
	sqlArguments := make([]interface{}, 0, noBaseArgs+len(res.GetQueryArguments()))
	sqlArguments = append(sqlArguments, arg.Limit, arg.Offset)
	sqlArguments = append(sqlArguments, res.GetQueryArguments()...)
	rows, err := db.Query(ctx, sql, sqlArguments...)
	if err != nil {
		return nil, fmt.Errorf("failed to convert CQL to SQL: %w", err)
	}
	defer rows.Close()
	var items []ListPatronRequestsRow
	for rows.Next() {
		var i ListPatronRequestsRow
		if err := rows.Scan(
			&i.ID,
			&i.Timestamp,
			&i.IllRequest,
			&i.State,
			&i.Side,
			&i.Patron,
			&i.RequesterSymbol,
			&i.SupplierSymbol,
			&i.Tenant,
			&i.RequesterReqID,
			&i.NeedsAttention,
			&i.LastAction,
			&i.LastActionOutcome,
			&i.LastActionResult,
			&i.Language,
			&i.Items,
			&i.FullCount,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
