package pr_db

import (
	"fmt"
	"strings"

	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
)

// ListOverdueBatchCandidates reads the fields needed to plan an overdue action.
// CQL controls eligibility; the request ID gives paging a stable key.
func (r *PgPrRepo) ListOverdueBatchCandidates(ctx common.ExtendedContext, limit int32, afterID string, query pgcql.Query) ([]PatronRequest, error) {
	if limit <= 0 || query == nil {
		return nil, fmt.Errorf("overdue batch selection requires CQL and a positive limit")
	}
	sql := listOverdueBatchCandidates
	pos := strings.Index(sql, "ORDER BY id")
	if pos == -1 {
		return nil, fmt.Errorf("overdue batch query missing ORDER BY id")
	}
	if where := query.GetWhereClause(); where != "" {
		sql = sql[:pos] + "AND (" + where + ")\n" + sql[pos:]
	}
	args := []any{limit, afterID}
	args = append(args, query.GetQueryArguments()...)
	rows, err := r.GetConnOrTx().Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("select overdue candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]PatronRequest, 0, limit)
	for rows.Next() {
		var pr PatronRequest
		if err := rows.Scan(&pr.ID, &pr.Side, &pr.State, &pr.IllRequest); err != nil {
			return nil, fmt.Errorf("read overdue candidate: %w", err)
		}
		candidates = append(candidates, pr)
	}
	return candidates, rows.Err()
}
