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
	CreateNoticeWithID(eventID string, id string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, parentId *string, target SignalTarget) (string, error)
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

// WithContext binds calls on the returned bus to ctx. Its listener and consumer
// handlers continue using the context supplied to Start.
func WithContext(bus EventBus, ctx common.ExtendedContext) EventBus {
	if postgres, ok := bus.(*PostgresEventBus); ok {
		scoped := *postgres
		scoped.ctx = ctx
		return &scoped
	}
	return bus
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
	var event Event
	var err error
	var workerID string
	var memberFinished bool
	creation := data.Signal == SignalTaskCreated || data.Signal == SignalNoticeCreated
	if db, ok := p.repo.(*PgEventRepo); ok && creation {
		var row GetEventForCreationSignalRow
		row, err = db.queries.GetEventForCreationSignal(p.ctx, db.Pool, data.Event)
		event, workerID, memberFinished = row.Event, row.WorkerEventID, row.BatchMemberFinished
	} else {
		event, err = p.repo.GetEvent(p.ctx, data.Event)
	}
	if err != nil {
		p.ctx.Logger().Error("failure loading event", "error", err, "eventId", data.Event, "signal", data.Signal, "target", data.Target)
		return
	}
	p.ctx.Logger().Debug("received event", "channel", EVENT_BUS_CHANNEL,
		"signal", data.Signal, "target", data.Target, "eventName", event.EventName, "eventType", event.EventType, "eventStatus", event.EventStatus)

	eventCtx := p.getEventContext(&event)
	// Durable tasks are dispatched by the batch queue. Their creation
	// notification, if any, is only for observers.
	finishNotice := workerID != "" && !memberFinished && data.Signal == SignalNoticeCreated
	if workerID == "" && creation {
		workerID = event.EventData.BatchWorkerID // Legacy notices carried ownership in their payload.
	}
	if workerID != "" && workerID != event.ID && !memberFinished {
		leased, cancel, err := p.delegatedBatchContext(eventCtx, workerID)
		switch {
		case err == nil:
			defer cancel()
			eventCtx = leased
		case errors.Is(err, pgx.ErrNoRows):
			if data.Signal == SignalNoticeCreated {
				if _, err := p.repo.ClaimEventForSignal(eventCtx, event.ID, data.Signal); err != nil && !errors.Is(err, pgx.ErrNoRows) {
					eventCtx.Logger().Warn("clear obsolete batch notice signal", "error", err)
				}
			}
			return
		default:
			eventCtx.Logger().Warn("load delegated batch lease", "error", err)
			return
		}
	}

	if data.Target == SignalObservers || data.Target == SignalAll {
		triggerHandlers(eventCtx, event, p.getHandlers(data.Signal, HandlerRoleObserver, event.EventName), data.Signal)
	}

	if data.Target == SignalConsumers || data.Target == SignalAll {
		claimedEvent, err := p.repo.ClaimEventForSignal(eventCtx, data.Event, data.Signal)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				eventCtx.Logger().Error("failure claiming event", "error", err, "signal", data.Signal, "target", data.Target)
			}
			return
		}
		triggerHandlers(eventCtx, claimedEvent, p.getHandlers(data.Signal, HandlerRoleConsumer, claimedEvent.EventName), data.Signal)
		if finishNotice {
			if err := p.finishBatchEvent(eventCtx, event.ID); err != nil && eventCtx.Err() == nil {
				eventCtx.Logger().Warn("finish batch notice", "error", err)
			}
		}
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
		parent, err := batchParent(p.ctx, eventRepo, parentId)
		if err != nil {
			return err
		}
		lastSignal := string(SignalTaskCreated)
		if target == SignalInline {
			lastSignal = ""
		}
		_, err = eventRepo.SaveEvent(p.ctx, SaveEventParams{
			ID:               id,
			IllTransactionID: illTransactionID,
			Timestamp:        getPgNow(),
			EventType:        EventTypeTask,
			EventName:        eventName,
			EventStatus:      EventStatusNew,
			EventData:        data,
			ParentID:         getPgText(parentId),
			LastSignal:       lastSignal,
			PatronRequestID:  patronRequestID,
		})
		if err != nil {
			return err
		}
		if parent.ID != "" {
			workerID := parent.WorkerEventID
			// Directly queued children get independent leases. Inline work,
			// observer-affine tasks, and their continuations stay with the
			// owner, which may be waiting for them in a batch worker slot.
			if workerID == *parentId && (target == SignalConsumers || target == SignalAll) {
				workerID = id
			}
			if err := eventRepo.(*PgEventRepo).addBatchEvent(p.ctx, id, parent.ID, workerID); err != nil {
				return err
			}
			if workerID == id {
				if target == SignalAll {
					if err := eventRepo.Notify(p.ctx, id, SignalTaskCreated, SignalObservers); err != nil {
						return err
					}
				}
				return eventRepo.Notify(p.ctx, id, SignalTaskCreated, SignalBatchWorkers)
			}
		}
		if target == SignalInline {
			return nil
		}
		return eventRepo.Notify(p.ctx, id, SignalTaskCreated, target)
	})
}

func (p *PostgresEventBus) CreateNotice(classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, target SignalTarget) (string, error) {
	return p.CreateNoticeWithID(uuid.NewString(), classId, eventName, data, status, eventDomain, nil, target)
}

func (p *PostgresEventBus) CreateNoticeWithParent(classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, parentId *string, target SignalTarget) (string, error) {
	return p.CreateNoticeWithID(uuid.NewString(), classId, eventName, data, status, eventDomain, parentId, target)
}

// CreateNoticeWithID publishes a notice using a caller-allocated event ID.
func (p *PostgresEventBus) CreateNoticeWithID(id string, classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, parentId *string, target SignalTarget) (string, error) {
	illTransactionID, patronRequestID := getIllTransactionAndPatronRequestId(classId, eventDomain)
	return id, p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		parent, err := batchParent(p.ctx, eventRepo, parentId)
		if err != nil {
			return err
		}
		_, err = eventRepo.SaveEvent(p.ctx, SaveEventParams{
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
		if parent.ID != "" {
			tx := eventRepo.(*PgEventRepo)
			if err := tx.addBatchEvent(p.ctx, id, parent.ID, parent.WorkerEventID); err != nil {
				return err
			}
			if target == SignalObservers {
				// Observer-only notices do not reserve work for consumers.
				if err := tx.queries.FinishBatchRunEvent(p.ctx, tx.Tx, id); err != nil {
					return err
				}
			}
		}
		return eventRepo.Notify(p.ctx, id, SignalNoticeCreated, target)
	})
}

func (p *PostgresEventBus) BeginTask(eventId string, target SignalTarget) (Event, error) {
	return p.beginTask(p.ctx, eventId, target)
}

func (p *PostgresEventBus) beginTask(ctx common.ExtendedContext, eventId string, target SignalTarget) (Event, error) {
	var event Event
	err := p.repo.WithTxFunc(ctx, func(eventRepo EventRepo) error {
		if _, err := lockOwnedBatchEvent(ctx, eventRepo, eventId); err != nil {
			return err
		}
		var err error
		event, err = eventRepo.GetEventForUpdate(ctx, eventId)
		if err != nil {
			return err
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
	return p.completeTask(p.ctx, eventId, result, status, target)
}

func (p *PostgresEventBus) completeTask(ctx common.ExtendedContext, eventId string, result *EventResult, status EventStatus, target SignalTarget) (Event, error) {
	var event Event
	var callbacks []func(common.ExtendedContext, Event)
	err := p.repo.WithTxFunc(ctx, func(eventRepo EventRepo) error {
		member, err := lockOwnedBatchEvent(ctx, eventRepo, eventId)
		if err != nil {
			return err
		}
		event, err = eventRepo.GetEventForUpdate(ctx, eventId)
		if err != nil {
			return err
		}
		if event.EventType != EventTypeTask {
			return fmt.Errorf("cannot complete task processing, event is not a TASK but %s", event.EventType)
		}
		if event.EventStatus != EventStatusProcessing {
			return fmt.Errorf("cannot complete task processing, event is not in state PROCESSING but %s", event.EventStatus)
		}
		event.EventStatus = status
		if result != nil {
			event.ResultData = *result
		}
		event.LastSignal = string(SignalTaskComplete)
		if member.ID != "" {
			if target == SignalConsumers || target == SignalAll {
				callbacks = p.getHandlers(SignalTaskComplete, HandlerRoleConsumer, event.EventName)
			}
			if len(callbacks) == 0 {
				event.LastSignal = ""
			}
		}
		event, err = eventRepo.SaveEvent(ctx, SaveEventParams(event))
		if err != nil {
			return err
		}
		if member.ID == "" {
			return eventRepo.Notify(ctx, eventId, SignalTaskComplete, target)
		}
		if target == SignalObservers || target == SignalAll {
			if err := eventRepo.Notify(ctx, eventId, SignalTaskComplete, SignalObservers); err != nil {
				return err
			}
		}
		if len(callbacks) == 0 {
			return eventRepo.(*PgEventRepo).finishBatchEvent(ctx, eventId, member)
		}
		return nil
	})
	if err != nil || len(callbacks) == 0 {
		return event, err
	}
	// The terminal result is visible to completion handlers. Keep membership
	// open until they have attached any successor work.
	triggerHandlers(ctx, event, callbacks, SignalTaskComplete)
	if err := p.finishBatchEvent(ctx, eventId); err != nil {
		return event, err
	}
	event.LastSignal = ""
	return event, nil
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
		event, err = p.beginTask(ctx, event.ID, target)
	}
	if err != nil {
		p.getEventContext(inEvent).Logger().Warn("failed to start processing TASK event", "error", err, "eventName", inEvent.EventName)
		return event, err
	}

	status, result := h(ctx, event)

	event, err = p.completeTask(ctx, event.ID, result, status, target)
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
