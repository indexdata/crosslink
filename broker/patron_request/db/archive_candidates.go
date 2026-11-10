package pr_db

import (
	"fmt"
	"strings"
	"time"

	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
)

// PatronRequestArchiveCursor identifies the last candidate examined in a stable
// created_at/ID scan, including candidates skipped or failed by the archiver.
type PatronRequestArchiveCursor struct {
	CreatedAt time.Time
	ID        string
}

// ListPatronRequestArchiveCandidates returns a bounded page without counting or
// loading full request payloads. CQL controls eligibility; pagination always
// uses created_at and ID so deletions do not shift subsequent pages.
// The caller must restrict CQL to the action's side and owner.
func (r *PgPrRepo) ListPatronRequestArchiveCandidates(ctx common.ExtendedContext, limit int32, after *PatronRequestArchiveCursor, query pgcql.Query) ([]PatronRequest, error) {
	if query == nil || limit <= 0 {
		return nil, fmt.Errorf("patron request archive selection requires CQL and a positive limit")
	}
	var cursor []string
	if after != nil {
		cursor = []string{after.CreatedAt.Format("2006-01-02T15:04:05.999999999"), after.ID}
	}
	sql := listPatronRequestArchiveCandidates
	if where := query.GetWhereClause(); where != "" {
		orderPos := strings.Index(sql, "ORDER BY")
		if orderPos < 0 {
			return nil, fmt.Errorf("patron request archive query is missing its ordering clause")
		}
		sql = sql[:orderPos] + "AND (" + where + ")\n" + sql[orderPos:]
	}
	args := []any{limit, cursor}
	args = append(args, query.GetQueryArguments()...)
	rows, err := r.GetConnOrTx().Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("select patron request archive candidates: %w", err)
	}
	defer rows.Close()
	var candidates []PatronRequest
	for rows.Next() {
		var pr PatronRequest
		if err := rows.Scan(&pr.ID, &pr.CreatedAt, &pr.State, &pr.UpdatedAt); err != nil {
			return nil, fmt.Errorf("read patron request archive candidate: %w", err)
		}
		candidates = append(candidates, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read patron request archive candidates: %w", err)
	}
	return candidates, nil
}
