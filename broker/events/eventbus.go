package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const EVENT_BUS_CHANNEL = "crosslink_channel"
const EB_COMP = "event_bus"
const DEFAULT_ILL_TRANSACTION_ID = "00000000-0000-0000-0000-000000000001"
const DEFAULT_PATRON_REQUEST_ID = "00000000-0000-0000-0000-000000000002"

type EventBus interface {
	Start(ctx common.ExtendedContext) error
	CreateTask(id string, eventName EventName, data EventData, eventDomain EventDomain, parentId *string, target SignalTarget) (string, error)
	CreateNotice(id string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, target SignalTarget) (string, error)
	// CreateNoticeWithID publishes a notice using a caller-allocated event ID.
	CreateNoticeWithID(eventID string, id string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, target SignalTarget) (string, error)
	// CreateNoticeWithParent creates a notice linked to a parent event.
	CreateNoticeWithParent(id string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, parentId *string, target SignalTarget) (string, error)
	// BeginTask marks a task as processing and emits SignalTaskBegin to the selected target.
	BeginTask(eventId string, target SignalTarget) (Event, error)
	// CompleteTask marks a task as finished and emits SignalTaskComplete to the selected target.
	CompleteTask(eventId string, result *EventResult, status EventStatus, target SignalTarget) (Event, error)
	// HandleEventCreated registers a handler for task/notice creation signal.
	HandleEventCreated(eventName EventName, role HandlerRole, f func(ctx common.ExtendedContext, event Event))
	// HandleTaskStarted registers a handler for task start signal.
	HandleTaskStarted(eventName EventName, role HandlerRole, f func(ctx common.ExtendedContext, event Event))
	// HandleTaskCompleted registers a handler for task completion signal.
	HandleTaskCompleted(eventName EventName, role HandlerRole, f func(ctx common.ExtendedContext, event Event))
	// ProcessTask runs h inside BeginTask/CompleteTask block and signals the selected target.
	ProcessTask(ctx common.ExtendedContext, event Event, target SignalTarget, h func(common.ExtendedContext, Event) (EventStatus, *EventResult)) (Event, error)
	// ProcessExclusiveTask runs h only when no older incomplete task exists for the same domain object and event name.
	ProcessExclusiveTask(ctx common.ExtendedContext, event Event, target SignalTarget, h func(common.ExtendedContext, Event) (EventStatus, *EventResult)) (Event, error)
	FindAncestor(descendant *Event, eventName EventName) *Event
	GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (Event, error)
}

type PostgresEventBus struct {
	repo             EventRepo
	ctx              common.ExtendedContext
	ConnectionString string
	handlersMu       *sync.RWMutex
	handlers         map[Signal]map[HandlerRole]map[EventName][]func(ctx common.ExtendedContext, event Event)
	batch            *batchWorkers
	randGen          *rand.Rand // local random generator to avoid same seed for all instance, only needed in Go < 1.20
}

func NewPostgresEventBus(repo EventRepo, connString string) *PostgresEventBus {
	return &PostgresEventBus{
		repo:             repo,
		ConnectionString: connString,
		handlersMu:       &sync.RWMutex{},
		handlers:         make(map[Signal]map[HandlerRole]map[EventName][]func(common.ExtendedContext, Event)),
		// #nosec G404 - math/rand is sufficient for connection jitter
		randGen: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *PostgresEventBus) Start(ctx common.ExtendedContext) error {
	p.ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(EB_COMP))
	conn, err := p.connectListener()
	if err != nil {
		return err
	}
	if db, ok := p.repo.(*PgEventRepo); ok {
		p.startBatchWorkers(db)
		go db.recoverMessageConfirmations(p.ctx)
	}
	go p.listen(conn)
	return nil
}

func (p *PostgresEventBus) connectListener() (*pgx.Conn, error) {
	ctx, cancel := context.WithTimeout(p.ctx, batchPollInterval)
	defer cancel()
	conn, err := pgx.Connect(ctx, p.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("connect event listener: %w", err)
	}
	if _, err = conn.Exec(ctx, "LISTEN "+EVENT_BUS_CHANNEL); err != nil {
		closeEventListener(conn)
		return nil, fmt.Errorf("listen on event channel: %w", err)
	}
	return conn, nil
}

func closeEventListener(conn *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), batchPollInterval)
	defer cancel()
	_ = conn.Close(ctx)
}

func (p *PostgresEventBus) listen(conn *pgx.Conn) {
	for {
		notification, err := conn.WaitForNotification(p.ctx)
		if err != nil {
			closeEventListener(conn)
			if p.ctx.Err() != nil {
				return
			}
			p.ctx.Logger().Warn("event listener disconnected", "error", err)
			conn = p.reconnectListener()
			if conn == nil {
				return
			}
			p.wakeBatchWorkers()
			continue
		}
		var data NotifyData
		if err := json.Unmarshal([]byte(notification.Payload), &data); err != nil {
			p.ctx.Logger().Error("decode event notification", "error", err)
			continue
		}
		if data.Target == SignalBatchWorkers {
			p.wakeBatchWorkers()
			continue
		}
		go p.handleNotify(data)
	}
}

func (p *PostgresEventBus) reconnectListener() *pgx.Conn {
	delay := time.Second
	for {
		jitter := time.Duration(p.randGen.Intn(500)) * time.Millisecond
		select {
		case <-time.After(delay + jitter):
		case <-p.ctx.Done():
			return nil
		}
		conn, err := p.connectListener()
		if err == nil {
			return conn
		}
		p.ctx.Logger().Warn("reconnect event listener", "error", err)
		delay = min(delay+delay/2, 30*time.Second)
	}
}

func (p *PostgresEventBus) handleNotify(data NotifyData) {
	event, err := p.repo.GetEvent(p.ctx, data.Event)
	if err != nil {
		p.ctx.Logger().Error("failure loading event", "error", err, "eventId", data.Event, "signal", data.Signal, "target", data.Target)
		return
	}
	p.ctx.Logger().Debug("received event", "channel", EVENT_BUS_CHANNEL,
		"signal", data.Signal,
		"target", data.Target,
		"eventName", event.EventName,
		"eventType", event.EventType,
		"eventStatus", event.EventStatus)

	if data.Target == SignalObservers || data.Target == SignalAll {
		triggerHandlers(p.getEventContext(&event), event, p.getHandlers(data.Signal, HandlerRoleObserver, event.EventName), data.Signal)
	}

	if data.Target == SignalConsumers || data.Target == SignalAll {
		eventCtx := p.getEventContext(&event)
		if event.EventData.BatchWorkerID != "" && (data.Signal == SignalTaskCreated || data.Signal == SignalNoticeCreated) {
			var cancel context.CancelFunc
			eventCtx, cancel, err = p.delegatedBatchContext(eventCtx, event.EventData.BatchWorkerID)
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					eventCtx.Logger().Warn("load delegated batch lease", "error", err)
				}
				return
			}
			defer cancel()
		}
		claimedEvent, claimErr := p.repo.ClaimEventForSignal(eventCtx, data.Event, data.Signal)
		if claimErr != nil {
			if !errors.Is(claimErr, pgx.ErrNoRows) {
				p.ctx.Logger().Error("failure claiming event", "error", claimErr, "eventId", data.Event, "signal", data.Signal, "target", data.Target)
			} else {
				p.ctx.Logger().Debug("no event claimed for signal", "eventId", data.Event, "signal", data.Signal, "target", data.Target)
			}
			return
		}
		triggerHandlers(eventCtx, claimedEvent, p.getHandlers(data.Signal, HandlerRoleConsumer, claimedEvent.EventName), data.Signal)
	}
}

func triggerHandlers(eventCtx common.ExtendedContext, event Event, handlers []func(ctx common.ExtendedContext, event Event), signal Signal) {
	var wg sync.WaitGroup
	if len(handlers) > 0 {
		eventCtx.Logger().Debug("found handlers for event", "count", len(handlers), "eventName", event.EventName, "signal", signal)
		for _, handler := range handlers {
			wg.Add(1)
			go func(h func(common.ExtendedContext, Event), e Event) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						eventCtx.Logger().Error("event handler panicked", "panic", r, "eventName", event.EventName, "signal", signal)
					}
				}()
				h(eventCtx.WithArgs(&common.LoggerArgs{TransactionId: event.IllTransactionID, EventId: event.ID}), e)
			}(handler, event)
		}
	} else {
		eventCtx.Logger().Debug("no handlers found for event", "eventName", event.EventName, "signal", signal)
	}
	wg.Wait() // Wait for all goroutines to finish
	eventCtx.Logger().Debug("all handlers finished", "eventName", event.EventName, "signal", signal)
}

func (p *PostgresEventBus) CreateTask(classId string, eventName EventName, data EventData, eventDomain EventDomain, parentId *string, target SignalTarget) (string, error) {
	id := uuid.New().String()
	illTransactionID, patronRequestID := getIllTransactionAndPatronRequestId(classId, eventDomain)
	return id, p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		var run BatchActionRun
		var workerID string
		if tx, ok := eventRepo.(*PgEventRepo); ok && parentId != nil &&
			(eventName == EventNameInvokeAction || eventName == EventNameInvokeBackgroundAction) {
			var err error
			run, err = tx.lockBatchRunForEvent(p.ctx, *parentId)
			if err != nil {
				return err
			}
			if run.ID != "" {
				if err := tx.checkBatchEventOwnership(p.ctx, *parentId); err != nil {
					return err
				}
				parent, err := tx.GetEvent(p.ctx, *parentId)
				if err != nil {
					return err
				}
				if run.CompletedAt.Valid || parent.EventStatus != EventStatusProcessing {
					return repo.ErrBatchLeaseLost
				}
				workerID = id
				if eventName == EventNameInvokeAction {
					workerID = repo.BatchWorker(p.ctx)
				}
			}
		}
		delegated := eventName == EventNameMessageRequester || eventName == EventNameMessageSupplier
		if tx, ok := eventRepo.(*PgEventRepo); ok && run.ID == "" {
			owner := repo.BatchWorker(p.ctx)
			if owner != "" && (delegated || eventName == EventNamePatronRequestMessage) {
				var err error
				run, err = tx.lockBatchRunForEvent(p.ctx, owner)
				if err != nil {
					return err
				}
				if run.ID == "" || run.CompletedAt.Valid {
					return repo.ErrBatchLeaseLost
				}
				workerID = owner
				if delegated {
					data.BatchWorkerID = owner
				}
			}
			if err := repo.LockBatchWorker(p.ctx, tx.Tx); err != nil {
				return err
			}
		}

		event, err := eventRepo.SaveEvent(p.ctx, SaveEventParams{
			ID:               id,
			IllTransactionID: illTransactionID,
			Timestamp:        getPgNow(),
			EventType:        EventTypeTask,
			EventName:        eventName,
			EventStatus:      EventStatusNew,
			EventData:        data,
			ParentID:         getPgText(parentId),
			LastSignal:       string(SignalTaskCreated),
			PatronRequestID:  patronRequestID,
		})
		if err != nil {
			return err
		}
		if run.ID != "" {
			tx := eventRepo.(*PgEventRepo)
			if err := tx.queries.AddBatchActionRunEvent(p.ctx, tx.Tx, AddBatchActionRunEventParams{
				EventID: id, RunID: run.ID, WorkerEventID: workerID,
			}); err != nil {
				return fmt.Errorf("link batch child task: %w", err)
			}
			if workerID == id {
				return eventRepo.Notify(p.ctx, id, SignalTaskCreated, SignalBatchWorkers)
			}
			if !delegated {
				// The current handler starts inline children synchronously.
				return nil
			}
		}
		err = eventRepo.Notify(p.ctx, id, SignalTaskCreated, target)
		p.ctx.Logger().Debug("created TASK event", "eventName", eventName, "eventId", event.ID, "status", event.EventStatus)
		return err
	})
}

func (p *PostgresEventBus) CreateNotice(classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, target SignalTarget) (string, error) {
	return p.CreateNoticeWithID(uuid.NewString(), classId, eventName, data, status, eventDomain, target)
}

func (p *PostgresEventBus) CreateNoticeWithParent(classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, parentId *string, target SignalTarget) (string, error) {
	return p.createNotice(uuid.NewString(), classId, eventName, data, status, eventDomain, parentId, target)
}

// CreateNoticeWithID publishes a notice using a caller-allocated event ID.
func (p *PostgresEventBus) CreateNoticeWithID(eventID string, classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, target SignalTarget) (string, error) {
	return p.createNotice(eventID, classId, eventName, data, status, eventDomain, nil, target)
}

func (p *PostgresEventBus) createNotice(id string, classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, parentId *string, target SignalTarget) (string, error) {
	illTransactionID, patronRequestID := getIllTransactionAndPatronRequestId(classId, eventDomain)
	return id, p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		if tx, ok := eventRepo.(*PgEventRepo); ok {
			if err := repo.LockBatchWorker(p.ctx, tx.Tx); err != nil {
				return err
			}
		}

		if status == EventStatusSuccess && (eventName == EventNameSupplierMsgReceived || eventName == EventNameRequesterMsgReceived) {
			data.BatchWorkerID = repo.BatchWorker(p.ctx)
		}
		event, err := eventRepo.SaveEvent(p.ctx, SaveEventParams{
			ID:               id,
			IllTransactionID: illTransactionID,
			Timestamp:        getPgNow(),
			EventType:        EventTypeNotice,
			EventName:        eventName,
			EventStatus:      status,
			EventData:        data,
			LastSignal:       string(SignalNoticeCreated),
			PatronRequestID:  patronRequestID,
			ParentID:         getPgText(parentId),
		})
		if err != nil {
			return err
		}
		err = eventRepo.Notify(p.ctx, id, SignalNoticeCreated, target)
		p.ctx.Logger().Debug("created NOTICE event", "eventName", eventName, "eventId", event.ID, "status", status)
		return err
	})
}

func (p *PostgresEventBus) BeginTask(eventId string, target SignalTarget) (Event, error) {
	return p.beginTask(p.ctx, Event{ID: eventId}, target)
}

func (p *PostgresEventBus) beginTask(ctx common.ExtendedContext, hint Event, target SignalTarget) (Event, error) {
	eventId := hint.ID
	var event Event
	err := p.repo.WithTxFunc(ctx, func(eventRepo EventRepo) error {
		checkOwnership := batchOwnershipRequired(ctx, hint)
		if tx, ok := eventRepo.(*PgEventRepo); ok && checkOwnership {
			if err := tx.checkBatchEventOwnership(ctx, eventId); err != nil {
				return err
			}
		}
		var err error
		event, err = eventRepo.GetEventForUpdate(ctx, eventId)
		if err != nil {
			return err
		}
		if !checkOwnership && batchOwnershipRequired(ctx, event) {
			return repo.ErrBatchLeaseLost
		}
		if event.EventType != EventTypeTask {
			return fmt.Errorf("cannot begin task processing, event is not a TASK but %s", event.EventType)
		}
		if event.EventStatus != EventStatusNew {
			return fmt.Errorf("cannot begin task processing, event is not in state NEW but %s", event.EventStatus)
		}
		event, err = eventRepo.UpdateEventLifecycle(ctx, UpdateEventLifecycleParams{
			ID: eventId, EventStatus: EventStatusProcessing, LastSignal: string(SignalTaskBegin),
		})
		if err != nil {
			return err
		}
		return eventRepo.Notify(ctx, eventId, SignalTaskBegin, target)
	})
	return event, err
}

func (p *PostgresEventBus) CompleteTask(eventId string, result *EventResult, status EventStatus, target SignalTarget) (Event, error) {
	return p.completeTask(p.ctx, Event{ID: eventId}, result, status, target)
}

func (p *PostgresEventBus) completeTask(ctx common.ExtendedContext, hint Event, result *EventResult, status EventStatus, target SignalTarget) (Event, error) {
	eventId := hint.ID
	var event Event
	err := p.repo.WithTxFunc(ctx, func(eventRepo EventRepo) error {
		tx, postgres := eventRepo.(*PgEventRepo)
		var run BatchActionRun
		var err error
		checkOwnership := batchOwnershipRequired(ctx, hint)
		if postgres && checkOwnership {
			run, err = tx.lockBatchRunForEvent(ctx, eventId)
			if err != nil {
				return err
			}
			if err = tx.checkBatchEventOwnership(ctx, eventId); err != nil {
				return err
			}
		}
		event, err = eventRepo.GetEventForUpdate(ctx, eventId)
		if err != nil {
			return err
		}
		if !checkOwnership && batchOwnershipRequired(ctx, event) {
			return repo.ErrBatchLeaseLost
		}
		if event.EventType != EventTypeTask {
			return fmt.Errorf("cannot complete task processing, event is not a TASK but %s", event.EventType)
		}
		if event.EventStatus != EventStatusProcessing {
			return fmt.Errorf("cannot complete task processing, event is not in state PROCESSING but %s", event.EventStatus)
		}
		event, err = completeEvent(ctx, eventRepo, event, result, status, target)
		if err != nil || run.ID == "" {
			return err
		}
		if err = tx.queries.FinishBatchRunEvent(ctx, tx.Tx, eventId); err != nil {
			return err
		}
		if err := tx.queries.ClearIdleBatchWorkerLease(ctx, tx.Tx, repo.BatchWorker(ctx)); err != nil {
			return err
		}
		return tx.finishBatchRunIfIdle(ctx, run)
	})
	return event, err
}

func completeEvent(ctx common.ExtendedContext, eventRepo EventRepo, event Event, result *EventResult, status EventStatus, target SignalTarget) (Event, error) {
	event.EventStatus = status
	if result != nil {
		event.ResultData = *result
	}
	event.LastSignal = string(SignalTaskComplete)
	event, err := eventRepo.SaveEvent(ctx, SaveEventParams(event))
	if err != nil {
		return event, err
	}
	return event, eventRepo.Notify(ctx, event.ID, SignalTaskComplete, target)
}

func (p *PostgresEventBus) HandleEventCreated(eventName EventName, role HandlerRole, f func(ctx common.ExtendedContext, event Event)) {
	p.registerHandler(SignalTaskCreated, role, eventName, f)
	p.registerHandler(SignalNoticeCreated, role, eventName, f)
}

func (p *PostgresEventBus) HandleTaskStarted(eventName EventName, role HandlerRole, f func(ctx common.ExtendedContext, event Event)) {
	p.registerHandler(SignalTaskBegin, role, eventName, f)
}

func (p *PostgresEventBus) HandleTaskCompleted(eventName EventName, role HandlerRole, f func(ctx common.ExtendedContext, event Event)) {
	p.registerHandler(SignalTaskComplete, role, eventName, f)
}

func (p *PostgresEventBus) ProcessTask(ctx common.ExtendedContext, event Event, target SignalTarget, h func(common.ExtendedContext, Event) (EventStatus, *EventResult)) (Event, error) {
	inEvent := &event
	var err error
	if event.ID == repo.BatchWorker(ctx) && event.EventStatus == EventStatusProcessing {
		if !consumeBatchClaim(ctx) {
			return event, repo.ErrBatchLeaseLost
		}
	} else {
		event, err = p.beginTask(ctx, event, target)
	}
	if err != nil {
		p.getEventContext(inEvent).Logger().Warn("failed to start processing TASK event", "error", err, "eventName", inEvent.EventName)
		return event, err
	}

	status, result := h(ctx, event)

	event, err = p.completeTask(ctx, event, result, status, target)
	if err != nil {
		p.getEventContext(inEvent).Logger().Warn("failed to complete processing TASK event", "error", err, "eventName", inEvent.EventName)
		return event, err
	}
	return event, nil
}

func (p *PostgresEventBus) ProcessExclusiveTask(ctx common.ExtendedContext, event Event, target SignalTarget, h func(common.ExtendedContext, Event) (EventStatus, *EventResult)) (Event, error) {
	var exclusivityCheckErr error
	event, err := p.ProcessTask(ctx, event, target, func(ctx common.ExtendedContext, event Event) (EventStatus, *EventResult) {
		conflict, err := p.repo.GetOlderIncompleteEvent(ctx, event)
		if err == nil {
			p.getEventContext(&event).Logger().Warn("exclusive task blocked by older incomplete task",
				"eventName", event.EventName,
				"conflictingEventId", conflict.ID,
				"conflictingEventStatus", conflict.EventStatus)
			return NewErrorResult(fmt.Sprintf("another %s task in progress", event.EventName), fmt.Sprintf("conflicting event %s", conflict.ID))
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			p.getEventContext(&event).Logger().Warn("failed to check exclusive TASK event", "error", err, "eventName", event.EventName)
			exclusivityCheckErr = err
			return NewErrorResult("failed to check exclusive task", err.Error())
		}

		return h(ctx, event)
	})
	if err != nil {
		return event, err
	}
	return event, exclusivityCheckErr
}

func (p *PostgresEventBus) registerHandler(signal Signal, role HandlerRole, eventName EventName, f func(ctx common.ExtendedContext, event Event)) {
	p.handlersMu.Lock()
	defer p.handlersMu.Unlock()
	if p.handlers[signal] == nil {
		p.handlers[signal] = make(map[HandlerRole]map[EventName][]func(ctx common.ExtendedContext, event Event))
	}
	if p.handlers[signal][role] == nil {
		p.handlers[signal][role] = make(map[EventName][]func(ctx common.ExtendedContext, event Event))
	}
	p.handlers[signal][role][eventName] = append(p.handlers[signal][role][eventName], f)
}

func (p *PostgresEventBus) getHandlers(signal Signal, role HandlerRole, eventName EventName) []func(ctx common.ExtendedContext, event Event) {
	p.handlersMu.RLock()
	defer p.handlersMu.RUnlock()
	signalHandlers, ok := p.handlers[signal]
	if !ok {
		return nil
	}
	roleHandlers, ok := signalHandlers[role]
	if !ok {
		return nil
	}
	return roleHandlers[eventName]
}

func (p *PostgresEventBus) FindAncestor(descendant *Event, ancestorName EventName) *Event {
	var event *Event
	parentId := getParentId(descendant)
	for parentId != nil {
		found, err := p.repo.GetEvent(p.ctx, *parentId)
		if err != nil {
			p.getEventContext(descendant).Logger().Warn("failed to get parent event", "eventName", ancestorName, "parentId", parentId, "error", err)
		} else if found.EventName == ancestorName {
			event = &found
			break
		} else {
			parentId = getParentId(&found)
		}
	}
	return event
}

func (p *PostgresEventBus) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (Event, error) {
	return p.repo.GetLatestRequestEventByAction(ctx, illTransId, action)
}

func (p *PostgresEventBus) getEventContext(event *Event) common.ExtendedContext {
	//TODO extend context with event name and status
	return p.ctx.WithArgs(&common.LoggerArgs{
		TransactionId: event.IllTransactionID,
		EventId:       event.ID,
		Component:     EB_COMP,
	})
}

func getParentId(event *Event) *string {
	if event != nil && event.ParentID.Valid {
		return &event.ParentID.String
	} else {
		return nil
	}
}
func getPgNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}

func getPgText(value *string) pgtype.Text {
	stringValue := ""
	if value != nil {
		stringValue = *value
	}
	return pgtype.Text{
		Valid:  value != nil,
		String: stringValue,
	}
}

func getIllTransactionAndPatronRequestId(classId string, eventDomain EventDomain) (string, string) {
	if eventDomain == EventDomainPatronRequest {
		return DEFAULT_ILL_TRANSACTION_ID, classId
	} else {
		return classId, DEFAULT_PATRON_REQUEST_ID
	}
}
