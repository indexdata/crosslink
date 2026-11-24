package sched_service

import (
	"fmt"
	"time"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/repo"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/indexdata/crosslink/iso18626"
)

// Overdue only queues normal exclusive actions. The action rechecks eligibility
// and sends before transitioning; failed sends remain eligible next time.
func (s *BatchActionService) Overdue(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	data := event.EventData.BatchActionData
	if data == nil || data.Selector == "" {
		return events.NewErrorResult("invalid overdue batch", "selector is required")
	}
	result := &events.EventResult{CustomData: map[string]any{}}
	queued, failed := 0, 0
	err := s.visitOverduePlan(ctx, event.ID, data.Selector, func(entry sched_db.BatchActionPlanEntry) {
		errText := entry.Error
		if errText == "" {
			if err := s.queuePlannedAction(ctx, event, entry, nil); err != nil {
				errText = err.Error()
			}
		}
		if errText == "" {
			queued++
			return
		}
		failed++
		if len(result.CustomData) < sched_db.BatchActionPlanPageSize {
			result.CustomData[entry.RequestID] = errText
		}
	})
	if err != nil {
		return events.NewErrorResult("cannot select overdue loans", err.Error())
	}
	result.Note = fmt.Sprintf("queued overdue actions for %d loans", queued)
	if failed > 0 {
		result.Note += fmt.Sprintf("; failed %d loans", failed)
		return events.EventStatusError, result
	}
	return events.EventStatusSuccess, result
}

func overdueQuery(selector string, cutoff time.Time) (pgcql.Query, error) {
	qb, err := cqlbuilder.NewQueryFromString(selector)
	if err != nil {
		return nil, err
	}
	query, err := qb.And().Search("side").Term(string(prservice.SideLending)).
		And().BeginClause().
		Search("service_type").Term(string(iso18626.TypeServiceTypeLoan)).
		Or().Search("service_type").Term(string(iso18626.TypeServiceTypeCopyOrLoan)).EndClause().
		And().Search("due_at").Rel(cql.LT).Term(cutoff.UTC().Format(time.RFC3339)).Build()
	if err != nil {
		return nil, err
	}
	return pr_db.ParsePatronRequestsCql(query.String())
}

func (s *BatchActionService) visitOverduePlan(ctx common.ExtendedContext, runID, selector string, visit func(sched_db.BatchActionPlanEntry)) error {
	coordinated := repo.BatchAttemptFrom(ctx).ID != ""
	progress := sched_db.OverduePlanProgress{Cutoff: time.Now().UTC()}
	if coordinated {
		var err error
		progress, err = s.schedRepo.StartOverdueBatchPlan(ctx, runID)
		if err != nil {
			return err
		}
		if progress.Complete {
			return s.visitSavedBatchPlan(ctx, runID, visit)
		}
	}
	query, err := overdueQuery(selector, progress.Cutoff)
	if err != nil {
		return fmt.Errorf("invalid overdue selector: %w", err)
	}

	for {
		// Do not filter needs_attention: failed sends must be retried on later runs.
		requests, err := s.prRepo.ListOverdueBatchCandidates(ctx, sched_db.BatchActionPlanPageSize, progress.Cursor, query)
		if err != nil {
			return err
		}
		entries := s.overduePlanEntries(requests)
		next := progress.Cursor
		if len(requests) > 0 {
			next = requests[len(requests)-1].ID
		}
		complete := len(requests) < sched_db.BatchActionPlanPageSize
		if coordinated {
			if err := s.schedRepo.SaveOverdueBatchPlanPage(ctx, runID, progress.Cursor, next, entries, complete); err != nil {
				return err
			}
		} else {
			for _, entry := range entries {
				visit(entry)
			}
		}
		if complete {
			break
		}
		progress.Cursor = next
	}
	if coordinated {
		return s.visitSavedBatchPlan(ctx, runID, visit)
	}
	return nil
}

func (s *BatchActionService) overduePlanEntries(requests []pr_db.PatronRequest) []sched_db.BatchActionPlanEntry {
	action := prservice.LenderActionOverdue
	entries := make([]sched_db.BatchActionPlanEntry, 0, len(requests))
	for _, request := range requests {
		mapping, err := s.actionMappingService.GetActionMapping(request.IllRequest)
		if err != nil {
			entries = append(entries, sched_db.BatchActionPlanEntry{RequestID: request.ID, Error: err.Error()})
		} else if mapping.IsActionSupported(request, action) {
			entries = append(entries, sched_db.BatchActionPlanEntry{RequestID: request.ID, Action: string(action)})
		}
	}
	return entries
}
