package sched_service

import (
	"fmt"
	"time"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
)

// IllTransactionArchiveRepo selects and atomically archives transaction-owned data.
type IllTransactionArchiveRepo interface {
	ListIllTransactionArchiveCandidates(common.ExtendedContext, int32, string, pgcql.Query) ([]ill_db.IllTransactionArchiveCandidate, error)
	ArchiveIllTransaction(common.ExtendedContext, ill_db.IllTransactionArchiveCandidate, string, time.Time, bool) (bool, error)
}

// WithIllTransactionArchiveRepo wires the transaction repository into batch execution.
func (s *BatchActionService) WithIllTransactionArchiveRepo(repo IllTransactionArchiveRepo) *BatchActionService {
	s.illArchiveRepo = repo
	return s
}

// ArchiveIllTransactions archives transactions without a local borrowing request.
func (s *BatchActionService) ArchiveIllTransactions(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	data := event.EventData.BatchActionData
	if data == nil || s.illArchiveRepo == nil {
		return events.NewErrorResult("invalid transaction archive", "batch data and archive repository are required")
	}
	if err := ValidateArchiveConfig(string(schedoapi.ArchiveIllTransactions), data.Owner, data.Selector, event.EventData.CustomData); err != nil {
		return events.NewErrorResult("invalid transaction archive", err.Error())
	}
	interval, err := time.ParseDuration(event.EventData.CustomData["interval"].(string))
	if err != nil {
		return events.NewErrorResult("invalid transaction archive", err.Error())
	}
	anonymize, _ := event.EventData.CustomData["anonymize"].(bool)
	cutoff := time.Now().UTC().Add(-interval)
	qb, err := cqlbuilder.NewQueryFromString(data.Selector)
	if err != nil {
		return events.NewErrorResult("invalid transaction archive selector", err.Error())
	}
	query, err := qb.And().Search("requester_symbol").Rel(cql.EXACT).Term(data.Owner).
		And().Search("timestamp").Rel(cql.LE).Term(cutoff.Format(TIME_FORMAT)).Build()
	if err != nil {
		return events.NewErrorResult("invalid transaction archive selector", err.Error())
	}
	pgquery, err := ill_db.ParseIllTransactionsCql(query.String(), 2)
	if err != nil {
		return events.NewErrorResult("invalid transaction archive selector", err.Error())
	}
	result := &events.EventResult{CustomData: map[string]any{}}
	archived, skipped, failed := 0, 0, 0
	cursor := ""
	var runErr error
	for archived < int(archiveBatchSize) {
		if runErr = ctx.Err(); runErr != nil {
			break
		}
		candidates, err := s.illArchiveRepo.ListIllTransactionArchiveCandidates(ctx, archiveBatchSize, cursor, pgquery)
		if err != nil {
			runErr = fmt.Errorf("select transaction archive candidates: %w", err)
			break
		}
		for _, candidate := range candidates {
			if runErr = ctx.Err(); runErr != nil {
				break
			}
			cursor = candidate.ID
			stored, err := s.illArchiveRepo.ArchiveIllTransaction(ctx, candidate, data.Owner, cutoff, anonymize)
			if err != nil {
				failed++
				if len(result.CustomData) < int(archiveBatchSize) {
					result.CustomData[candidate.ID] = err.Error()
				}
			} else if stored {
				archived++
			} else {
				skipped++
			}
			if archived == int(archiveBatchSize) {
				break
			}
		}
		if runErr != nil || len(candidates) < int(archiveBatchSize) {
			break
		}
	}
	result.Note = fmt.Sprintf("archived %d ILL transactions; skipped %d changed, locally borrowed or active transactions; failed %d transactions", archived, skipped, failed)
	if runErr != nil {
		result.EventError = &events.EventError{Message: "transaction archive interrupted", Cause: runErr.Error()}
	}
	if runErr != nil || failed > 0 {
		return events.EventStatusError, result
	}
	return events.EventStatusSuccess, result
}
