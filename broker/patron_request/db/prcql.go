package pr_db

import (
	"context"
	"fmt"
	"strings"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
)

func handlePatronRequestsQuery(cqlString string, noBaseArgs int) (pgcql.Query, error) {
	def := pgcql.NewPgDefinition()

	f := pgcql.NewFieldString().WithExact()
	def.AddField("state", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("side", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("requester_symbol", f)

	f = pgcql.NewFieldString().WithExact()
	def.AddField("supplier_symbol", f)

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
	noBaseArgs := 2 // weh have two base arguments: limit and offset
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
			&i.PatronRequest.ID,
			&i.PatronRequest.Timestamp,
			&i.PatronRequest.IllRequest,
			&i.PatronRequest.State,
			&i.PatronRequest.Side,
			&i.PatronRequest.Patron,
			&i.PatronRequest.RequesterSymbol,
			&i.PatronRequest.SupplierSymbol,
			&i.PatronRequest.Tenant,
			&i.PatronRequest.RequesterReqID,
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
