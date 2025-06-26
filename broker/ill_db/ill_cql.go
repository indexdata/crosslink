package ill_db

import (
	"context"
	"strings"
)

func (q *Queries) ListIllTransactionsCql(ctx context.Context, db DBTX, arg ListIllTransactionsParams, cql *string) ([]ListIllTransactionsRow, error) {
	if cql == nil {
		return q.ListIllTransactions(ctx, db, arg)
	}
	pos := strings.Index(listIllTransactions, "ORDER BY")
	// TODO parse CQL and get where clause
	whereClause := ""
	sql := listIllTransactions[:pos] + whereClause + listIllTransactions[pos:]
	rows, err := db.Query(ctx, sql, arg.Limit, arg.Offset)
	if err != nil {
		return nil, err
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
