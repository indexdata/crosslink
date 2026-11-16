package sched_service

import (
	"fmt"
	"strconv"
	"time"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/go-utils/utils"
)

const BATCH_COMP = "batch_action"
const TIME_FORMAT = "2006-01-02 15:04:05"

var BATCH_ACTION_RUN_RETENTION = common.ToInt32(utils.Must(utils.GetEnvInt("BATCH_ACTION_RUN_RETENTION", 5)))

type BatchActionService struct {
	eventBus             events.EventBus
	prRepo               pr_db.PrRepo
	prArchiveRepo        PatronRequestArchiveRepo
	illArchiveRepo       IllTransactionArchiveRepo
	schedRepo            sched_db.SchedRepo
	emailSenderService   *EmailSenderService
	actionMappingService prservice.ActionMappingService
}

// PatronRequestArchiveRepo selects archive candidates and archives each side's
// owned data atomically.
type PatronRequestArchiveRepo interface {
	ListPatronRequestArchiveCandidates(common.ExtendedContext, int32, *pr_db.PatronRequestArchiveCursor, pgcql.Query) ([]pr_db.PatronRequest, error)
	ArchiveBorrowingRequest(common.ExtendedContext, pr_db.PatronRequest, string, time.Time, bool) (bool, error)
	ArchiveLendingRequest(common.ExtendedContext, pr_db.PatronRequest, string, time.Time, bool) (bool, error)
}

func NewBatchActionService(eventBus events.EventBus, prRepo pr_db.PrRepo, schedRepo sched_db.SchedRepo, emailSenderService *EmailSenderService, archiveRepos ...PatronRequestArchiveRepo) *BatchActionService {
	s := &BatchActionService{
		eventBus:             eventBus,
		prRepo:               prRepo,
		schedRepo:            schedRepo,
		emailSenderService:   emailSenderService,
		actionMappingService: prservice.ActionMappingService{SMService: &prservice.StateModelService{}},
	}
	if len(archiveRepos) > 0 {
		s.prArchiveRepo = archiveRepos[0]
	}
	return s
}

func (s *BatchActionService) BatchAction(ctx common.ExtendedContext, event events.Event) {
	_, _ = s.eventBus.ProcessTask(ctx, event, events.SignalConsumers, s.batchAction)
}
func (s *BatchActionService) batchAction(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(BATCH_COMP))
	if event.EventData.BatchActionData == nil {
		ctx.Logger().Error("batch action data is empty", "eventId", event.ID)
		return events.NewErrorResult("cannot process event", "batch action data is empty")
	}

	var action func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)
	switch event.EventData.BatchActionData.ActionName {
	case string(schedoapi.EmailPullslips):
		action = s.emailSenderService.EmailPullslip
	case string(schedoapi.RequestAging):
		action = s.RequestAging
	case string(schedoapi.Overdue):
		action = s.Overdue
	case string(schedoapi.ArchiveBorrowingRequests):
		action = s.ArchiveBorrowingRequests
	case string(schedoapi.ArchiveLendingRequests):
		action = s.ArchiveLendingRequests
	case string(schedoapi.ArchiveIllTransactions):
		action = s.ArchiveIllTransactions
	default:
		ctx.Logger().Error("unknown batch action",
			"actionName", event.EventData.BatchActionData.ActionName,
			"eventId", event.ID,
			"taskId", event.EventData.BatchActionData.TaskId)
		return events.NewErrorResult("cannot process event", "unknown batch action")
	}

	var restrictedSelector string
	var err error
	switch event.EventData.BatchActionData.ActionName {
	case string(schedoapi.ArchiveIllTransactions):
		// Transaction execution applies its own CQL and requester-owner restriction.
		restrictedSelector = event.EventData.BatchActionData.Selector
	case string(schedoapi.ArchiveBorrowingRequests):
		restrictedSelector, err = addPatronRequestArchiveRestriction(event.EventData.BatchActionData.Selector, event.EventData.BatchActionData.Owner, prservice.SideBorrowing)
	case string(schedoapi.ArchiveLendingRequests):
		restrictedSelector, err = addPatronRequestArchiveRestriction(event.EventData.BatchActionData.Selector, event.EventData.BatchActionData.Owner, prservice.SideLending)
	default:
		restrictedSelector, err = addBatchActionOwnerRestriction(event.EventData.BatchActionData.Selector, event.EventData.BatchActionData.Owner)
	}
	if err != nil {
		return events.NewErrorResult("invalid batch action data", err.Error())
	}

	// Keep the event stored by the event bus unchanged while ensuring every
	// action handler receives the owner-restricted selector.
	batchActionData := *event.EventData.BatchActionData
	batchActionData.Selector = restrictedSelector
	event.EventData.BatchActionData = &batchActionData
	s.cleanupOldRuns(ctx, event)
	return action(ctx, event)
}

func addPatronRequestArchiveRestriction(selector, owner string, side pr_db.PatronRequestSide) (string, error) {
	if selector == "" || owner == "" {
		return "", fmt.Errorf("patron request archive requires a selector and owner")
	}
	ownerField := "requester_symbol_exact"
	switch side {
	case prservice.SideBorrowing:
	case prservice.SideLending:
		ownerField = "supplier_symbol_exact"
	default:
		return "", fmt.Errorf("invalid archive side %q", side)
	}
	qb, err := cqlbuilder.NewQueryFromString(selector)
	if err != nil {
		return "", err
	}
	query, err := qb.And().Search("side").Term(string(side)).
		And().Search(ownerField).Term(owner).Build()
	if err != nil {
		return "", err
	}
	return query.String(), nil
}

func (s *BatchActionService) cleanupOldRuns(ctx common.ExtendedContext, event events.Event) {
	if BATCH_ACTION_RUN_RETENTION <= 0 {
		return
	}
	taskID := event.EventData.BatchActionData.TaskId
	if taskID == "" {
		return
	}
	if err := s.schedRepo.DeleteOldBatchActionRunEvents(ctx, event.ID, taskID, BATCH_ACTION_RUN_RETENTION); err != nil {
		ctx.Logger().Error("failed to cleanup old batch action runs", "taskId", taskID, "retention", BATCH_ACTION_RUN_RETENTION, "error", err)
	}
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
	restrictedSelector, err := qb.And().
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
			processedCount++
			var action *pr_db.PatronRequestAction
			actionMapping, mappingErr := s.actionMappingService.GetActionMapping(pr.IllRequest)
			if mappingErr != nil {
				result.CustomData[pr.ID] = "could not find action mapping for patron request: " + pr.ID + ", error: " + mappingErr.Error()
				continue
			}
			action = actionMapping.GetClosingAction(pr)
			if action == nil {
				result.CustomData[pr.ID] = "could not find closing action for patron request state: " + string(pr.State) + " within state model: " + actionMapping.StateModelName
				continue
			}
			childBatchActionData := *batchActionData
			data := events.EventData{CommonEventData: events.CommonEventData{
				Action:          action,
				BatchActionData: &childBatchActionData,
			}, CustomData: backgroundActionParams(event.EventData.CustomData)}
			_, eventErr := s.eventBus.CreateTask(pr.ID, events.EventNameInvokeBackgroundAction, data, events.EventDomainPatronRequest, &event.ID, events.SignalConsumers)
			if eventErr != nil {
				result.CustomData[pr.ID] = "error creating close action: " + eventErr.Error()
			}
		}
	}
	result.Note = "processed patron request count: " + strconv.Itoa(processedCount)
	status := events.EventStatusSuccess
	if len(result.CustomData) > 0 {
		status = events.EventStatusError
		result.Note += ", failed: " + strconv.Itoa(len(result.CustomData)) + " with ids and errors in custom data"
	}
	return status, result
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
