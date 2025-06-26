package ill_db

import (
	"context"
	"fmt"
	"strings"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
)

func (q *Queries) ListIllTransactionsCql(ctx context.Context, db DBTX, arg ListIllTransactionsParams, cqlString *string) ([]ListIllTransactionsRow, error) {
	if cqlString == nil {
		return q.ListIllTransactions(ctx, db, arg)
	}
	noBaseArgs := 2 // weh have two base arguments: limit and offset

	def := pgcql.NewPgDefinition()

	f := &pgcql.FieldString{}
	f.WithExact().SetColumn("last_supplier_status")
	def.AddField("last_supplier_status", f)

	f = &pgcql.FieldString{}
	f.WithExact().SetColumn("id")
	def.AddField("id", f)

	f = &pgcql.FieldString{}
	f.WithExact().SetColumn("requester_symbol")
	def.AddField("requester_symbol", f)

	f = &pgcql.FieldString{}
	f.WithExact().SetColumn("supplier_symbol")
	def.AddField("supplier_symbol", f)

	var parser cql.Parser
	query, err := parser.Parse(*cqlString)
	if err != nil {
		return nil, err
	}
	res, err := def.Parse(query, noBaseArgs+1)
	if err != nil {
		return nil, err
	}
	whereClause := ""
	if res.GetWhereClause() != "" {
		whereClause = "WHERE " + res.GetWhereClause() + " "
	}

	pos := strings.Index(listIllTransactions, "ORDER BY")
	if pos == -1 {
		return nil, fmt.Errorf("CQL query must contain an ORDER BY clause")
	}
	sql := listIllTransactions[:pos] + whereClause + listIllTransactions[pos:]
	sqlArguments := make([]interface{}, 0, noBaseArgs+len(res.GetQueryArguments()))
	sqlArguments = append(sqlArguments, arg.Limit, arg.Offset)
	sqlArguments = append(sqlArguments, res.GetQueryArguments()...)
	rows, err := db.Query(ctx, sql, sqlArguments...)
	if err != nil {
		return nil, fmt.Errorf("failed to convert CQL to SQL: %w", err)
	}
	defer rows.Close()
	var items []ListIllTransactionsRow
	for rows.Next() {
		var i ListIllTransactionsRow
		if err := rows.Scan(
			&i.IllTransaction.ID,
			&i.IllTransaction.Timestamp,
			&i.IllTransaction.RequesterSymbol,
			&i.IllTransaction.RequesterID,
			&i.IllTransaction.LastRequesterAction,
			&i.IllTransaction.PrevRequesterAction,
			&i.IllTransaction.SupplierSymbol,
			&i.IllTransaction.RequesterRequestID,
			&i.IllTransaction.PrevRequesterRequestID,
			&i.IllTransaction.SupplierRequestID,
			&i.IllTransaction.LastSupplierStatus,
			&i.IllTransaction.PrevSupplierStatus,
			&i.IllTransaction.IllTransactionData,
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
