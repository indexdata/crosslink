package sched_service

import (
	"fmt"
	"time"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
)

const archiveBatchSize int32 = 100

// ArchiveBorrowingRequests stores a bounded set of inactive borrowing requests.
// Each repository call atomically writes one bundle and removes its live rows.
func (s *BatchActionService) ArchiveBorrowingRequests(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	return s.archivePatronRequests(ctx, event, prservice.SideBorrowing)
}

// ArchiveLendingRequests stores a bounded set of inactive supplier-owned requests
// without archiving transaction data or borrowing requests.
func (s *BatchActionService) ArchiveLendingRequests(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	return s.archivePatronRequests(ctx, event, prservice.SideLending)
}

func (s *BatchActionService) archivePatronRequests(ctx common.ExtendedContext, event events.Event, side pr_db.PatronRequestSide) (events.EventStatus, *events.EventResult) {
	label := string(side)
	data := event.EventData.BatchActionData
	if data == nil || data.Owner == "" || data.Selector == "" || s.prArchiveRepo == nil {
		return events.NewErrorResult("invalid "+label+" archive", "owner, selector, and archive repository are required")
	}
	archive := s.prArchiveRepo.ArchiveBorrowingRequest
	if side == prservice.SideLending {
		archive = s.prArchiveRepo.ArchiveLendingRequest
	}
	intervalText, ok := event.EventData.CustomData["interval"].(string)
	if !ok {
		return events.NewErrorResult("invalid "+label+" archive", "interval is required")
	}
	interval, err := time.ParseDuration(intervalText)
	if err != nil || interval <= 0 {
		return events.NewErrorResult("invalid "+label+" archive", "interval must be a positive Go duration")
	}
	anonymize := false
	if value, exists := event.EventData.CustomData["anonymize"]; exists {
		var valid bool
		anonymize, valid = value.(bool)
		if !valid {
			return events.NewErrorResult("invalid "+label+" archive", "anonymize must be a boolean")
		}
	}
	cutoff := time.Now().UTC().Add(-interval)
	qb, err := cqlbuilder.NewQueryFromString(data.Selector)
	if err != nil {
		return events.NewErrorResult("invalid "+label+" archive selector", err.Error())
	}
	query, err := qb.And().Search("updated_at").Rel(cql.LE).Term(cutoff.Format(TIME_FORMAT)).Build()
	if err != nil {
		return events.NewErrorResult("invalid "+label+" archive selector", err.Error())
	}
	pgcql, err := pr_db.ParsePatronRequestsCql(query.String())
	if err != nil {
		return events.NewErrorResult("invalid "+label+" archive selector", err.Error())
	}
	result := &events.EventResult{CustomData: map[string]any{}}
	archived, skipped, failed := 0, 0, 0
	var cursor *pr_db.PatronRequestArchiveCursor
	var runErr error
	for archived < int(archiveBatchSize) {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		prs, err := s.prArchiveRepo.ListPatronRequestArchiveCandidates(ctx, archiveBatchSize, cursor, pgcql)
		if err != nil {
			runErr = fmt.Errorf("select %s requests: %w", label, err)
			break
		}
		for _, pr := range prs {
			if err := ctx.Err(); err != nil {
				runErr = err
				break
			}
			cursor = &pr_db.PatronRequestArchiveCursor{CreatedAt: pr.CreatedAt.Time, ID: pr.ID}
			stored, archiveErr := archive(ctx, pr, data.Owner, cutoff, anonymize)
			if archiveErr != nil {
				failed++
				// Bound history size even when a large backlog repeatedly fails.
				if len(result.CustomData) < int(archiveBatchSize) {
					result.CustomData[pr.ID] = archiveErr.Error()
				}
				continue
			}
			if stored {
				archived++
				if archived == int(archiveBatchSize) {
					break
				}
			} else {
				skipped++
			}
		}
		if runErr != nil || len(prs) < int(archiveBatchSize) {
			break
		}
	}
	result.Note = fmt.Sprintf("archived %d %s requests; skipped %d changed or active requests; failed %d requests", archived, label, skipped, failed)
	if runErr != nil {
		result.EventError = &events.EventError{Message: label + " archive interrupted", Cause: runErr.Error()}
	}
	if failed > 0 || runErr != nil {
		return events.EventStatusError, result
	}
	return events.EventStatusSuccess, result
}
