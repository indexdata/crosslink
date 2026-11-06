package ill_db

import (
	"fmt"
	"strings"
	"time"

	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// IllTransactionArchiveCandidate contains a stable ID and the selected row version.
// Version must be rechecked under lock before archiving.
type IllTransactionArchiveCandidate struct {
	ID      string
	Version string
}

// ListIllTransactionArchiveCandidates scans IDs without counts or full payloads.
// The caller must restrict query to the action owner and age cutoff. after is the
// last examined ID, including skipped/failed candidates, so deletions cannot shift pages.
func (r *PgIllRepo) ListIllTransactionArchiveCandidates(ctx common.ExtendedContext, limit int32, after string, query pgcql.Query) ([]IllTransactionArchiveCandidate, error) {
	if query == nil || limit <= 0 {
		return nil, fmt.Errorf("transaction archive selection requires CQL and a positive limit")
	}
	sql := listIllTransactionArchiveCandidates
	if where := query.GetWhereClause(); where != "" {
		pos := strings.Index(sql, "ORDER BY")
		if pos < 0 {
			return nil, fmt.Errorf("transaction archive query is missing its ordering clause")
		}
		sql = sql[:pos] + "AND (" + where + ")\n" + sql[pos:]
	}
	args := []any{limit, after}
	args = append(args, query.GetQueryArguments()...)
	rows, err := r.GetConnOrTx().Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("select transaction archive candidates: %w", err)
	}
	defer rows.Close()
	var candidates []IllTransactionArchiveCandidate
	for rows.Next() {
		var candidate IllTransactionArchiveCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Version); err != nil {
			return nil, fmt.Errorf("read transaction archive candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transaction archive candidates: %w", err)
	}
	return candidates, nil
}

// ArchiveIllTransaction atomically archives an unchanged eligible transaction and
// removes its live rows. False means it changed, is active, or is locally borrowed.
func (r *PgIllRepo) ArchiveIllTransaction(ctx common.ExtendedContext, candidate IllTransactionArchiveCandidate, owner string, cutoff time.Time, anonymize bool) (bool, error) {
	archived, err := r.queries.ArchiveIllTransaction(ctx, r.GetConnOrTx(), ArchiveIllTransactionParams{ID: candidate.ID, Owner: owner, Version: candidate.Version, Cutoff: pgtype.Timestamp{Time: cutoff, Valid: true}, Anonymize: anonymize})
	if err != nil {
		return false, fmt.Errorf("archive transaction %s: %w", candidate.ID, err)
	}
	return archived, nil
}
