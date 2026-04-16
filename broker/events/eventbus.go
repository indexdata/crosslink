package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/indexdata/crosslink/broker/common"

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
	// Create a regular (unicast) task event
	CreateTask(id string, eventName EventName, data EventData, eventDomain EventDomain, parentId *string) (string, error)
	// Create a task event whose created signal is delivered to every event bus instance
	CreateTaskBroadcast(id string, eventName EventName, data EventData, eventDomain EventDomain, parentId *string) (string, error)
	// Create a regular (unicast) notice event
	CreateNotice(id string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain) (string, error)
	// Create a notice event whose created signal is delivered to every event bus instance
	CreateNoticeBroadcast(id string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain) (string, error)
	// Create a regular (unicast) notice event with parentId, for cases where notice is related to a parent task. If parentId is not needed, use CreateNotice
	CreateNoticeWithParent(id string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, parentId *string) (string, error)
	// Mark task for processing or fail if status is invalid (e.g already started)
	BeginTask(eventId string) (Event, error)
	BeginTaskBroadcast(eventId string) (Event, error)
	// Mark task as completed or fail if status is invalid (e.g not started)
	CompleteTask(eventId string, result *EventResult, status EventStatus) (Event, error)
	CompleteTaskBroadcast(eventId string, result *EventResult, status EventStatus) (Event, error)
	// Register handler for event (task or notice) created signal
	HandleEventCreated(eventName EventName, f func(ctx common.ExtendedContext, event Event))
	// Register observer handler for broadcast event created signal
	HandleEventCreatedBroadcast(eventName EventName, f func(ctx common.ExtendedContext, event Event))
	// Register handler for task started signal
	HandleTaskStarted(eventName EventName, f func(ctx common.ExtendedContext, event Event))
	// Register observer handler for broadcast task started signal
	HandleTaskStartedBroadcast(eventName EventName, f func(ctx common.ExtendedContext, event Event))
	// Register handler for task completed signal
	HandleTaskCompleted(eventName EventName, f func(ctx common.ExtendedContext, event Event))
	// Register observer handler for broadcast task completed signal
	HandleTaskCompletedBroadcast(eventName EventName, f func(ctx common.ExtendedContext, event Event))
	// Execute task processing function within an automatic Begin/Complete block.
	ProcessTask(ctx common.ExtendedContext, event Event, h func(common.ExtendedContext, Event) (EventStatus, *EventResult)) (Event, error)
	ProcessTaskBroadcast(ctx common.ExtendedContext, event Event, h func(common.ExtendedContext, Event) (EventStatus, *EventResult)) (Event, error)
	FindAncestor(descendant *Event, eventName EventName) *Event
	GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (Event, error)
}

type PostgresEventBus struct {
	repo                           EventRepo
	ctx                            common.ExtendedContext
	ConnectionString               string
	EventCreatedHandlers           map[EventName][]func(ctx common.ExtendedContext, event Event)
	BroadcastEventCreatedHandlers  map[EventName][]func(ctx common.ExtendedContext, event Event)
	TaskStartedHandlers            map[EventName][]func(ctx common.ExtendedContext, event Event)
	TaskCompletedHandlers          map[EventName][]func(ctx common.ExtendedContext, event Event)
	BroadcastTaskStartedHandlers   map[EventName][]func(ctx common.ExtendedContext, event Event)
	BroadcastTaskCompletedHandlers map[EventName][]func(ctx common.ExtendedContext, event Event)
	randGen                        *rand.Rand // local random generator to avoid same seed for all instance, only needed in Go < 1.20
}

func NewPostgresEventBus(repo EventRepo, connString string) *PostgresEventBus {
	return &PostgresEventBus{
		repo:             repo,
		ConnectionString: connString,
		// #nosec G404 - math/rand is sufficient for connection jitter
		randGen: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *PostgresEventBus) Start(ctx common.ExtendedContext) error {
	p.ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(EB_COMP))
	var conn *pgx.Conn
	var err error

	connectAndListen := func() error {
		conn, err = pgx.Connect(ctx, p.ConnectionString)
		if err != nil {
			ctx.Logger().Error("unable to connect to database", "error", err)
			return err
		}

		_, err = conn.Exec(ctx, "LISTEN "+EVENT_BUS_CHANNEL)
		if err != nil {
			ctx.Logger().Error("unable to listen to channel", "channel", EVENT_BUS_CHANNEL, "error", err)
			return err
		}

		ctx.Logger().Info("successfully connected and listening to channel", "channel", EVENT_BUS_CHANNEL)
		return nil
	}

	if err = connectAndListen(); err != nil {
		return err
	}

	go func() {
		for {
			notification, er := conn.WaitForNotification(ctx)
			if er != nil {
				ctx.Logger().Error("unable to receive notification", "error", err, "channel", EVENT_BUS_CHANNEL)

				if er.Error() == "conn closed" {
					ctx.Logger().Warn("connection closed, attempting to reconnect")

					baseDelay := 1 * time.Second
					maxDelay := 30 * time.Second
					delay := baseDelay

					for {
						// add random duration for jitter between instances
						jitter := time.Duration(p.randGen.Intn(500)) * time.Millisecond

						select {
						case <-time.After(delay + jitter):
							// Wait for the delay and continue to retry
						case <-ctx.Done():
							ctx.Logger().Info("context cancelled during reconnect, stopping retries")
							return // Exit goroutine if parent context is cancelled
						}

						if err = connectAndListen(); err == nil {
							ctx.Logger().Info("successfully reconnected")
							break // Exit the retry loop on success
						}
						ctx.Logger().Error("reconnection attempt failed", "error", err, "next_try_in", delay)

						// Gradually increase the delay for the next attempt
						delay = time.Duration(float64(delay) * 1.5)
						if delay > maxDelay {
							delay = maxDelay
						}
					}
				}
				if strings.Contains(er.Error(), "context canceled") {
					ctx.Logger().Error("context cancelled, terminating")
					break
				}
				continue
			}

			var notifyData NotifyData
			var err = json.Unmarshal([]byte(notification.Payload), &notifyData)
			if err != nil {
				ctx.Logger().Error("failed to unmarshal notification", "error", err, "payload", notification.Payload)
			}
			go p.handleNotify(notifyData)
		}
	}()
	return nil
}

func (p *PostgresEventBus) handleNotify(data NotifyData) {
	if data.Broadcast {
		p.handleBroadcastNotify(data)
		if data.Signal == SignalTaskCreated || data.Signal == SignalNoticeCreated {
			return
		}
	}

	event, err := p.repo.ClaimEventForSignal(p.ctx, data.Event, data.Signal)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			p.ctx.Logger().Error("failure claiming event", "error", err, "eventId", data.Event, "signal", data.Signal)
		} else {
			p.ctx.Logger().Debug("no event claimed for signal", "eventId", data.Event, "signal", data.Signal)
		}
		return
	}
	p.ctx.Logger().Debug("received event", "channel", EVENT_BUS_CHANNEL,
		"signal", data.Signal,
		"eventName", event.EventName,
		"eventType", event.EventType,
		"eventStatus", event.EventStatus)
	switch data.Signal {
	case SignalTaskCreated, SignalNoticeCreated:
		triggerHandlers(p.getEventContext(&event), event, p.EventCreatedHandlers, data.Signal)
	case SignalTaskBegin:
		triggerHandlers(p.getEventContext(&event), event, p.TaskStartedHandlers, data.Signal)
	case SignalTaskComplete:
		triggerHandlers(p.getEventContext(&event), event, p.TaskCompletedHandlers, data.Signal)
	default:
		p.ctx.Logger().Error("unsupported signal", "signal", data.Signal, "eventName", event.EventName)
	}
}

func (p *PostgresEventBus) handleBroadcastNotify(data NotifyData) {
	event, err := p.repo.GetEvent(p.ctx, data.Event)
	if err != nil {
		p.ctx.Logger().Error("failure loading broadcast event", "error", err, "eventId", data.Event, "signal", data.Signal)
		return
	}
	p.ctx.Logger().Debug("received broadcast event", "channel", EVENT_BUS_CHANNEL,
		"signal", data.Signal,
		"eventName", event.EventName,
		"eventType", event.EventType,
		"eventStatus", event.EventStatus)
	switch data.Signal {
	case SignalTaskCreated, SignalNoticeCreated:
		triggerHandlers(p.getEventContext(&event), event, p.BroadcastEventCreatedHandlers, data.Signal)
	case SignalTaskBegin:
		triggerHandlers(p.getEventContext(&event), event, p.BroadcastTaskStartedHandlers, data.Signal)
	case SignalTaskComplete:
		triggerHandlers(p.getEventContext(&event), event, p.BroadcastTaskCompletedHandlers, data.Signal)
	}
}

func triggerHandlers(eventCtx common.ExtendedContext, event Event, handlersMap map[EventName][]func(ctx common.ExtendedContext, event Event), signal Signal) {
	var wg sync.WaitGroup
	handlers, ok := handlersMap[event.EventName]
	if ok {
		eventCtx.Logger().Debug("found handlers for event", "count", len(handlers), "eventName", event.EventName, "signal", signal)
		for _, handler := range handlers {
			wg.Add(1)
			go func(h func(common.ExtendedContext, Event), e Event) {
				defer wg.Done()
				h(eventCtx.WithArgs(&common.LoggerArgs{TransactionId: event.IllTransactionID, EventId: event.ID}), e)
			}(handler, event)
		}
	} else {
		eventCtx.Logger().Debug("no handlers found for event", "eventName", event.EventName, "signal", signal)
	}
	wg.Wait() // Wait for all goroutines to finish
	eventCtx.Logger().Debug("all handlers finished", "eventName", event.EventName, "signal", signal)
}

func (p *PostgresEventBus) CreateTask(classId string, eventName EventName, data EventData, eventDomain EventDomain, parentId *string) (string, error) {
	return p.createTask(classId, eventName, data, eventDomain, parentId, false)
}

func (p *PostgresEventBus) CreateTaskBroadcast(illTransactionID string, eventName EventName, data EventData, eventDomain EventDomain, parentId *string) (string, error) {
	return p.createTask(illTransactionID, eventName, data, eventDomain, parentId, true)
}

func (p *PostgresEventBus) createTask(classId string, eventName EventName, data EventData, eventDomain EventDomain, parentId *string, broadcast bool) (string, error) {
	id := uuid.New().String()
	illTransactionID, patronRequestID := getIllTransactionAndPatronRequestId(classId, eventDomain)
	return id, p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
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
		if err != nil && event.ParentID.Valid {
			return err
		}
		if broadcast {
			err = eventRepo.NotifyBroadcast(p.ctx, id, SignalTaskCreated)
		} else {
			err = eventRepo.Notify(p.ctx, id, SignalTaskCreated)
		}
		p.ctx.Logger().Debug("created TASK event", "eventName", eventName, "eventId", event.ID, "status", event.EventStatus)
		return err
	})
}

func (p *PostgresEventBus) CreateNotice(classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain) (string, error) {
	return p.createNotice(classId, eventName, data, status, eventDomain, false, nil)
}

func (p *PostgresEventBus) CreateNoticeWithParent(classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, parentId *string) (string, error) {
	return p.createNotice(classId, eventName, data, status, eventDomain, false, parentId)
}

func (p *PostgresEventBus) CreateNoticeBroadcast(classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain) (string, error) {
	return p.createNotice(classId, eventName, data, status, eventDomain, true, nil)
}

func (p *PostgresEventBus) createNotice(classId string, eventName EventName, data EventData, status EventStatus, eventDomain EventDomain, broadcast bool, parentId *string) (string, error) {
	id := uuid.New().String()
	illTransactionID, patronRequestID := getIllTransactionAndPatronRequestId(classId, eventDomain)
	return id, p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
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
		if broadcast {
			err = eventRepo.NotifyBroadcast(p.ctx, id, SignalNoticeCreated)
		} else {
			err = eventRepo.Notify(p.ctx, id, SignalNoticeCreated)
		}
		p.ctx.Logger().Debug("created NOTICE event", "eventName", eventName, "eventId", event.ID, "status", status)
		return err
	})
}

func (p *PostgresEventBus) BeginTask(eventId string) (Event, error) {
	return p.beginTask(eventId, false)
}

func (p *PostgresEventBus) BeginTaskBroadcast(eventId string) (Event, error) {
	return p.beginTask(eventId, true)
}

func (p *PostgresEventBus) beginTask(eventId string, broadcast bool) (Event, error) {
	var event Event
	err := p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		var err error
		event, err = eventRepo.GetEventForUpdate(p.ctx, eventId)
		if err != nil {
			return err
		}
		if event.EventType != EventTypeTask {
			return fmt.Errorf("cannot begin task processing, event is not a TASK but %s", event.EventType)
		}
		if event.EventStatus != EventStatusNew {
			return fmt.Errorf("cannot begin task processing, event is not in state NEW but %s", event.EventStatus)
		}
		event, err = eventRepo.UpdateEventLifecycle(p.ctx, UpdateEventLifecycleParams{
			ID:          eventId,
			EventStatus: EventStatusProcessing,
			LastSignal:  string(SignalTaskBegin),
		})
		if err != nil {
			return err
		}
		if broadcast {
			err = eventRepo.NotifyBroadcast(p.ctx, eventId, SignalTaskBegin)
		} else {
			err = eventRepo.Notify(p.ctx, eventId, SignalTaskBegin)
		}
		return err
	})
	return event, err
}

func (p *PostgresEventBus) CompleteTask(eventId string, result *EventResult, status EventStatus) (Event, error) {
	return p.completeTask(eventId, result, status, false)
}

func (p *PostgresEventBus) CompleteTaskBroadcast(eventId string, result *EventResult, status EventStatus) (Event, error) {
	return p.completeTask(eventId, result, status, true)
}

func (p *PostgresEventBus) completeTask(eventId string, result *EventResult, status EventStatus, broadcast bool) (Event, error) {
	var event Event
	err := p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		var err error
		event, err = eventRepo.GetEventForUpdate(p.ctx, eventId)
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
		event, err = eventRepo.SaveEvent(p.ctx, SaveEventParams(event))
		if err != nil {
			return err
		}
		if broadcast {
			err = eventRepo.NotifyBroadcast(p.ctx, eventId, SignalTaskComplete)
		} else {
			err = eventRepo.Notify(p.ctx, eventId, SignalTaskComplete)
		}
		return err
	})
	return event, err
}

func (p *PostgresEventBus) HandleEventCreated(eventName EventName, f func(ctx common.ExtendedContext, event Event)) {
	if p.EventCreatedHandlers == nil {
		p.EventCreatedHandlers = make(map[EventName][]func(ctx common.ExtendedContext, event Event))
	}
	if p.EventCreatedHandlers[eventName] == nil {
		p.EventCreatedHandlers[eventName] = []func(ctx common.ExtendedContext, event Event){}
	}
	p.EventCreatedHandlers[eventName] = append(p.EventCreatedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleEventCreatedBroadcast(eventName EventName, f func(ctx common.ExtendedContext, event Event)) {
	if p.BroadcastEventCreatedHandlers == nil {
		p.BroadcastEventCreatedHandlers = make(map[EventName][]func(ctx common.ExtendedContext, event Event))
	}
	if p.BroadcastEventCreatedHandlers[eventName] == nil {
		p.BroadcastEventCreatedHandlers[eventName] = []func(ctx common.ExtendedContext, event Event){}
	}
	p.BroadcastEventCreatedHandlers[eventName] = append(p.BroadcastEventCreatedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleTaskStarted(eventName EventName, f func(ctx common.ExtendedContext, event Event)) {
	if p.TaskStartedHandlers == nil {
		p.TaskStartedHandlers = make(map[EventName][]func(ctx common.ExtendedContext, event Event))
	}
	if p.TaskStartedHandlers[eventName] == nil {
		p.TaskStartedHandlers[eventName] = []func(ctx common.ExtendedContext, event Event){}
	}
	p.TaskStartedHandlers[eventName] = append(p.TaskStartedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleTaskStartedBroadcast(eventName EventName, f func(ctx common.ExtendedContext, event Event)) {
	if p.BroadcastTaskStartedHandlers == nil {
		p.BroadcastTaskStartedHandlers = make(map[EventName][]func(ctx common.ExtendedContext, event Event))
	}
	if p.BroadcastTaskStartedHandlers[eventName] == nil {
		p.BroadcastTaskStartedHandlers[eventName] = []func(ctx common.ExtendedContext, event Event){}
	}
	p.BroadcastTaskStartedHandlers[eventName] = append(p.BroadcastTaskStartedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleTaskCompleted(eventName EventName, f func(ctx common.ExtendedContext, event Event)) {
	if p.TaskCompletedHandlers == nil {
		p.TaskCompletedHandlers = make(map[EventName][]func(ctx common.ExtendedContext, event Event))
	}
	if p.TaskCompletedHandlers[eventName] == nil {
		p.TaskCompletedHandlers[eventName] = []func(ctx common.ExtendedContext, event Event){}
	}
	p.TaskCompletedHandlers[eventName] = append(p.TaskCompletedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleTaskCompletedBroadcast(eventName EventName, f func(ctx common.ExtendedContext, event Event)) {
	if p.BroadcastTaskCompletedHandlers == nil {
		p.BroadcastTaskCompletedHandlers = make(map[EventName][]func(ctx common.ExtendedContext, event Event))
	}
	if p.BroadcastTaskCompletedHandlers[eventName] == nil {
		p.BroadcastTaskCompletedHandlers[eventName] = []func(ctx common.ExtendedContext, event Event){}
	}
	p.BroadcastTaskCompletedHandlers[eventName] = append(p.BroadcastTaskCompletedHandlers[eventName], f)
}

func (p *PostgresEventBus) ProcessTask(ctx common.ExtendedContext, event Event, h func(common.ExtendedContext, Event) (EventStatus, *EventResult)) (Event, error) {
	return p.processTask(ctx, event, h, p.BeginTask, p.CompleteTask)
}

func (p *PostgresEventBus) ProcessTaskBroadcast(ctx common.ExtendedContext, event Event, h func(common.ExtendedContext, Event) (EventStatus, *EventResult)) (Event, error) {
	return p.processTask(ctx, event, h, p.BeginTaskBroadcast, p.CompleteTaskBroadcast)
}

func (p *PostgresEventBus) processTask(ctx common.ExtendedContext, event Event, h func(common.ExtendedContext, Event) (EventStatus, *EventResult), begin func(eventId string) (Event, error), complete func(eventId string, result *EventResult, status EventStatus) (Event, error)) (Event, error) {
	inEvent := &event
	event, err := begin(event.ID)
	if err != nil {
		p.getEventContext(inEvent).Logger().Warn("failed to start processing TASK event", "error", err, "eventName", inEvent.EventName)
		return event, err
	}

	status, result := h(ctx, event)

	event, err = complete(event.ID, result, status)
	if err != nil {
		p.getEventContext(inEvent).Logger().Warn("failed to complete processing TASK event", "error", err, "eventName", inEvent.EventName)
		return event, err
	}
	return event, nil
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
