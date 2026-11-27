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
	"github.com/indexdata/crosslink/iso18626"
)

// Overdue only queues normal exclusive actions. The action rechecks eligibility
// and sends before transitioning; failed sends remain eligible next time.
func (s *BatchActionService) Overdue(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	data := event.EventData.BatchActionData
	if data == nil || data.Selector == "" {
		return events.NewErrorResult("invalid overdue batch", "selector is required")
	}
	qb, err := cqlbuilder.NewQueryFromString(data.Selector)
	if err != nil {
		return events.NewErrorResult("invalid overdue selector", err.Error())
	}
	selector, err := qb.And().Search("side").Term(string(prservice.SideLending)).
		And().BeginClause().
		Search("service_type").Term(string(iso18626.TypeServiceTypeLoan)).
		Or().Search("service_type").Term(string(iso18626.TypeServiceTypeCopyOrLoan)).EndClause().
		And().Search("due_at").Rel(cql.LT).Term(time.Now().UTC().Format(time.RFC3339)).Build()
	if err != nil {
		return events.NewErrorResult("invalid overdue selector", err.Error())
	}
	query, err := pr_db.ParsePatronRequestsCql(selector.String())
	if err != nil {
		return events.NewErrorResult("invalid overdue selector", err.Error())
	}
	// Do not filter needs_attention: failed sends remain eligible next time.
	const pageSize = 250
	result := &events.EventResult{CustomData: map[string]any{}}
	action := prservice.LenderActionOverdue
	queued, failed := 0, 0
	afterID := ""
	for {
		requests, err := s.prRepo.ListOverdueBatchCandidates(ctx, pageSize, afterID, query)
		if err != nil {
			return events.NewErrorResult("cannot select overdue loans", err.Error())
		}
		for _, request := range requests {
			if err := ctx.Err(); err != nil {
				return events.NewErrorResult("overdue batch interrupted", err.Error())
			}
			mapping, err := s.actionMappingService.GetActionMapping(request.IllRequest)
			if err == nil {
				if !mapping.IsActionSupported(request, action) {
					continue
				}
				child := *data
				_, err = s.eventBus.CreateTask(request.ID, events.EventNameInvokeBackgroundAction,
					events.EventData{CommonEventData: events.CommonEventData{Action: &action, BatchActionData: &child}},
					events.EventDomainPatronRequest, &event.ID, events.SignalConsumers)
			}
			if err != nil {
				failed++
				if len(result.CustomData) < pageSize {
					result.CustomData[request.ID] = err.Error()
				}
				continue
			}
			queued++
		}
		if len(requests) < pageSize {
			break
		}
		afterID = requests[len(requests)-1].ID
	}
	result.Note = fmt.Sprintf("queued overdue actions for %d loans", queued)
	if failed > 0 {
		result.Note += fmt.Sprintf(", failed to queue %d loans", failed)
		return events.EventStatusError, result
	}
	return events.EventStatusSuccess, result
}
