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
	// Do not filter needs_attention: failed sends must be retried on later runs.
	requests, _, err := s.prRepo.ListPatronRequests(ctx, pr_db.ListPatronRequestsParams{Limit: 2147483647}, query)
	if err != nil {
		return events.NewErrorResult("cannot select overdue loans", err.Error())
	}
	result := &events.EventResult{CustomData: map[string]any{}}
	action := prservice.LenderActionOverdue
	queued := 0
	for _, request := range requests {
		mapping, err := s.actionMappingService.GetActionMapping(request.IllRequest)
		if err != nil {
			result.CustomData[request.ID] = err.Error()
			continue
		}
		if !mapping.IsActionSupported(request, action) {
			continue
		}
		child := *data
		_, err = s.eventBus.CreateTask(request.ID, events.EventNameInvokeBackgroundAction,
			events.EventData{CommonEventData: events.CommonEventData{Action: &action, BatchActionData: &child}},
			events.EventDomainPatronRequest, &event.ID, events.SignalConsumers)
		if err != nil {
			result.CustomData[request.ID] = err.Error()
			continue
		}
		queued++
	}
	result.Note = fmt.Sprintf("queued overdue actions for %d loans", queued)
	if len(result.CustomData) > 0 {
		return events.EventStatusError, result
	}
	return events.EventStatusSuccess, result
}
