package ill_db

import (
	"context"
	"fmt"
	"strings"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
)

func handleIllTransactionsQuery(cqlString string, noBaseArgs int) (pgcql.Query, error) {
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
	query, err := parser.Parse(cqlString)
	if err != nil {
		return nil, err
	}
	return def.Parse(query, noBaseArgs+1)
}

func handlePeersQuery(cqlString string, noBaseArgs int) (pgcql.Query, error) {
	def := pgcql.NewPgDefinition()

	f := &pgcql.FieldString{}
	f.WithSplit().SetColumn("symbol_value")
	def.AddField("symbol", f)

	f = &pgcql.FieldString{}
	f.WithExact().SetColumn("id")
	def.AddField("id", f)

	var parser cql.Parser
	query, err := parser.Parse(cqlString)
	if err != nil {
		return nil, err
	}
	return def.Parse(query, noBaseArgs+1)
}

func (q *Queries) ListIllTransactionsCql(ctx context.Context, db DBTX, arg ListIllTransactionsParams,
	cqlString *string) ([]ListIllTransactionsRow, error) {
	if cqlString == nil {
		return q.ListIllTransactions(ctx, db, arg)
	}
	noBaseArgs := 2 // weh have two base arguments: limit and offset
	res, err := handleIllTransactionsQuery(*cqlString, noBaseArgs)
	if err != nil {
		return nil, err
	}
	whereClause := ""
	if res.GetWhereClause() != "" {
		whereClause = "WHERE " + res.GetWhereClause() + " "
	}
	orgSql := listIllTransactions
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

func (q *Queries) GetIllTransactionsByRequesterSymbolCql(ctx context.Context, db DBTX, arg GetIllTransactionsByRequesterSymbolParams,
	cqlString *string) ([]GetIllTransactionsByRequesterSymbolRow, error) {
	if cqlString == nil {
		return q.GetIllTransactionsByRequesterSymbol(ctx, db, arg)
	}
	noBaseArgs := 3 // we have three base arguments: requester_symbol, limit and offset
	res, err := handleIllTransactionsQuery(*cqlString, noBaseArgs)
	if err != nil {
		return nil, err
	}
	whereClause := ""
	if res.GetWhereClause() != "" {
		whereClause = "AND (" + res.GetWhereClause() + ") "
	}
	orgSql := getIllTransactionsByRequesterSymbol
	pos := strings.Index(orgSql, "ORDER BY")
	if pos == -1 {
		return nil, fmt.Errorf("CQL query must contain an ORDER BY clause")
	}
	sql := orgSql[:pos] + whereClause + orgSql[pos:]
	sqlArguments := make([]interface{}, 0, noBaseArgs+len(res.GetQueryArguments()))
	sqlArguments = append(sqlArguments, arg.RequesterSymbol, arg.Limit, arg.Offset)
	sqlArguments = append(sqlArguments, res.GetQueryArguments()...)

	rows, err := db.Query(ctx, sql, sqlArguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetIllTransactionsByRequesterSymbolRow
	for rows.Next() {
		var i GetIllTransactionsByRequesterSymbolRow
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

func (q *Queries) ListPeersCql(ctx context.Context, db DBTX, arg ListPeersParams, cqlString *string) ([]ListPeersRow, error) {
	if cqlString == nil {
		return q.ListPeers(ctx, db, arg)
	}
	noBaseArgs := 2 // we have two base arguments: limit and offset
	res, err := handlePeersQuery(*cqlString, noBaseArgs)
	if err != nil {
		return nil, err
	}
	whereClause := ""
	if res.GetWhereClause() != "" {
		whereClause = "JOIN symbol ON peer_id = id WHERE " + res.GetWhereClause() + " "
	}
	orgSql := listPeers
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
		return nil, err
	}
	defer rows.Close()
	var items []ListPeersRow
	for rows.Next() {
		var i ListPeersRow
		if err := rows.Scan(
			&i.Peer.ID,
			&i.Peer.Name,
			&i.Peer.RefreshPolicy,
			&i.Peer.RefreshTime,
			&i.Peer.Url,
			&i.Peer.LoansCount,
			&i.Peer.BorrowsCount,
			&i.Peer.Vendor,
			&i.Peer.BrokerMode,
			&i.Peer.CustomData,
			&i.Peer.HttpHeaders,
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
