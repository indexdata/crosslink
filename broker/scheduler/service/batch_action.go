package sched_service

import (
	"strconv"
	"time"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	prapi "github.com/indexdata/crosslink/broker/patron_request/api"
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
	if event.EventData.BatchActionData != nil {
		switch event.EventData.BatchActionData.ActionName {
		case string(schedoapi.EmailPullslips):
			return s.emailSenderService.EmailPullslip(ctx, event)
		case string(schedoapi.RequestAging):
			return s.RequestAging(ctx, event)
		default:
			ctx.Logger().Error("unknown batch action", "actionName", event.EventData.BatchActionData.ActionName, "event", event)
			return events.NewErrorResult("cannot process event", "unknown batch action")
		}
	}
	ctx.Logger().Error("batch action data is empty", "event", event.ID)
	return events.NewErrorResult("cannot process event", "batch action data is empty")
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
	if batchActionData.Owner != "" {
		var side pr_db.PatronRequestSide
		qb, err = prapi.AddOwnerRestriction(qb, batchActionData.Owner, side)
		if err != nil {
			return events.NewErrorResult("failed to add owner restriction", err.Error())
		}
	}
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
			var action = prservice.LenderActionCannotSupply
			childBatchActionData := *batchActionData
			data := events.EventData{CommonEventData: events.CommonEventData{
				Action:          &action,
				BatchActionData: &childBatchActionData,
			}, CustomData: backgroundActionParams(event.EventData.CustomData)}
			_, eventErr := s.eventBus.CreateTask(pr.ID, events.EventNameInvokeBackgroundAction, data, events.EventDomainPatronRequest, &event.ID, events.SignalConsumers)
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
