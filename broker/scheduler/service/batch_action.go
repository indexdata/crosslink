package sched_service

import (
	"fmt"
	"strconv"
	"time"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
)

const BATCH_COMP = "batch_action"
const TIME_FORMAT = "2006-01-02 15:04:05"

type BatchActionService struct {
	eventBus           events.EventBus
	prRepo             pr_db.PrRepo
	emailSenderService *EmailSenderService
}

func NewBatchActionService(eventBus events.EventBus, prRepo pr_db.PrRepo, emailSenderService *EmailSenderService) *BatchActionService {
	return &BatchActionService{
		eventBus:           eventBus,
		prRepo:             prRepo,
		emailSenderService: emailSenderService,
	}
}
func (s *BatchActionService) BatchAction(ctx common.ExtendedContext, event events.Event) {
	_, _ = s.eventBus.ProcessTask(ctx, event, events.SignalConsumers, s.batchAction)
}
func (s *BatchActionService) batchAction(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(BATCH_COMP))
	if event.EventData.BatchActionData == nil {
		ctx.Logger().Error("batch action data is empty", "event", event.ID)
		return events.NewErrorResult("cannot process event", "batch action data is empty")
	}

	var action func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)
	switch event.EventData.BatchActionData.ActionName {
	case string(schedoapi.EmailPullslips):
		action = s.emailSenderService.EmailPullslip
	case string(schedoapi.RequestAging):
		action = s.RequestAging
	default:
		ctx.Logger().Error("unknown batch action", "actionName", event.EventData.BatchActionData.ActionName, "event", event)
		return events.NewErrorResult("cannot process event", "unknown batch action")
	}

	restrictedSelector, err := addBatchActionOwnerRestriction(
		event.EventData.BatchActionData.Selector,
		event.EventData.BatchActionData.Owner,
	)
	if err != nil {
		return events.NewErrorResult("invalid batch action data", err.Error())
	}

	// Keep the event stored by the event bus unchanged while ensuring every
	// action handler receives the owner-restricted selector.
	batchActionData := *event.EventData.BatchActionData
	batchActionData.Selector = restrictedSelector
	event.EventData.BatchActionData = &batchActionData
	return action(ctx, event)
}

func addBatchActionOwnerRestriction(selector string, owner string) (string, error) {
	if selector == "" {
		return "", fmt.Errorf("selector is empty")
	}
	if owner == "" {
		return selector, nil
	}

	qb, err := cqlbuilder.NewQueryFromString(selector)
	if err != nil {
		return "", err
	}
	_, err = qb.And().
		BeginClause().
		Search("side").Term(string(prservice.SideLending)).
		And().Search("supplier_symbol_exact").Term(owner).
		Or().
		BeginClause().Search("side").Term(string(prservice.SideBorrowing)).
		And().Search("requester_symbol_exact").Term(owner).
		EndClause().
		EndClause().
		Build()
	if err != nil {
		return "", err
	}
	restrictedSelector, err := qb.Build()
	if err != nil {
		return "", err
	}
	return restrictedSelector.String(), nil
}

func (s *BatchActionService) RequestAging(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	if event.EventData.BatchActionData == nil {
		return events.NewErrorResult("cannot process event", "batch action data is empty")
	}
	batchActionData := event.EventData.BatchActionData

	if batchActionData.Selector == "" {
		return events.NewErrorResult("cannot process event", "selector is empty")
	}
	intervalString, ok := event.EventData.CustomData["interval"].(string)
	if !ok || intervalString == "" {
		return events.NewErrorResult("cannot process event", "interval is missing or not a string")
	}
	interval, err := time.ParseDuration(intervalString)
	if err != nil {
		return events.NewErrorResult("cannot process event", "interval is invalid")
	}

	qb, err := cqlbuilder.NewQueryFromString(batchActionData.Selector)
	if err != nil {
		return events.NewErrorResult("invalid cql selector", err.Error())
	}

	fromTime := time.Now().UTC().Add(-interval).Format(TIME_FORMAT)
	qb.And().Search("updated_at").Rel(cql.LE).Term(fromTime)
	builtCQL, err := qb.Build()
	if err != nil {
		return events.NewErrorResult("invalid cql selector", err.Error())
	}
	pgcql, err := pr_db.ParsePatronRequestsCql(builtCQL.String())
	if err != nil {
		return events.NewErrorResult("invalid cql selector", err.Error())
	}

	prs, _, err := s.prRepo.ListPatronRequests(ctx, pr_db.ListPatronRequestsParams{Limit: MAX_RECORDS_PER_EMAIL, Offset: 0}, pgcql)
	if err != nil {
		return events.NewErrorResult("did not select data for processing", err.Error())
	}

	var result = &events.EventResult{CustomData: map[string]any{}}
	var processedCount = 0
	if len(prs) > 0 {
		for _, pr := range prs {
			var action = prservice.BorrowerActionCancelRequest
			if pr.Side == prservice.SideLending {
				action = prservice.LenderActionCannotSupply
			}
			data := events.EventData{CommonEventData: events.CommonEventData{
				Action: &action,
			}, CustomData: backgroundActionParams(event.EventData.CustomData)}
			_, eventErr := s.eventBus.CreateTask(pr.ID, events.EventNameInvokeBackgroundAction, data, events.EventDomainPatronRequest, nil, events.SignalConsumers)
			if eventErr != nil {
				result.CustomData[pr.ID] = "error creating close action: " + eventErr.Error()
			}
			processedCount++
		}
	}
	result.Note = "processed patron request count: " + strconv.Itoa(processedCount)

	return events.EventStatusSuccess, result
}

func backgroundActionParams(customData map[string]any) map[string]any {
	if len(customData) == 0 {
		return nil
	}
	params := make(map[string]any, len(customData))
	for key, value := range customData {
		if key == "interval" {
			continue
		}
		params[key] = value
	}
	if len(params) == 0 {
		return nil
	}
	return params
}
