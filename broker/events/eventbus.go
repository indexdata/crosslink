package events

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	extctx "github.com/indexdata/crosslink/broker/common"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const EVENT_BUS_CHANNEL = "crosslink_channel"

type EventBus interface {
	Start(ctx extctx.ExtendedContext) error
	CreateTask(illTransactionID string, eventName EventName, data EventData, parentId *string) (string, error)
	CreateNotice(illTransactionID string, eventName EventName, data EventData, status EventStatus) (string, error)
	BeginTask(eventId string) error
	CompleteTask(eventId string, result *EventResult, status EventStatus) error
	HandleEventCreated(eventName EventName, f func(ctx extctx.ExtendedContext, event Event))
	HandleTaskStarted(eventName EventName, f func(ctx extctx.ExtendedContext, event Event))
	HandleTaskCompleted(eventName EventName, f func(ctx extctx.ExtendedContext, event Event))
	ProcessTask(ctx extctx.ExtendedContext, event Event, h func(extctx.ExtendedContext, Event) (EventStatus, *EventResult))
	FindAncestor(ctx extctx.ExtendedContext, descendant *Event, eventName EventName) *Event
}

type PostgresEventBus struct {
	repo                  EventRepo
	ctx                   extctx.ExtendedContext
	ConnectionString      string
	EventCreatedHandlers  map[EventName][]func(ctx extctx.ExtendedContext, event Event)
	TaskStartedHandlers   map[EventName][]func(ctx extctx.ExtendedContext, event Event)
	TaskCompletedHandlers map[EventName][]func(ctx extctx.ExtendedContext, event Event)
}

func NewPostgresEventBus(repo EventRepo, connString string) *PostgresEventBus {
	return &PostgresEventBus{
		repo:             repo,
		ConnectionString: connString,
	}
}

func (p *PostgresEventBus) Start(ctx extctx.ExtendedContext) error {
	p.ctx = ctx
	var conn *pgx.Conn
	var err error

	connectAndListen := func() error {
		conn, err = pgx.Connect(ctx, p.ConnectionString)
		if err != nil {
			ctx.Logger().Error("event_bus: unable to connect to database", "error", err)
			return err
		}

		_, err = conn.Exec(ctx, "LISTEN "+EVENT_BUS_CHANNEL)
		if err != nil {
			ctx.Logger().Error("event_bus: unable to listen to channel "+EVENT_BUS_CHANNEL, "error", err)
			return err
		}

		ctx.Logger().Info("event_bus: successfully connected and listening to channel " + EVENT_BUS_CHANNEL)
		return nil
	}

	if err = connectAndListen(); err != nil {
		return err
	}

	go func() {
		for {
			notification, er := conn.WaitForNotification(ctx)
			if er != nil {
				ctx.Logger().Error("event_bus: unable to receive notification", "error", err, "channel", EVENT_BUS_CHANNEL)

				if er.Error() == "conn closed" {
					ctx.Logger().Warn("event_bus: connection closed, attempting to reconnect")

					for attempt := 1; ; attempt++ {
						time.Sleep(time.Duration(attempt) * time.Second)
						if err = connectAndListen(); err == nil {
							break
						}
						ctx.Logger().Error("event_bus: reconnection attempt failed", "error", err, "attempt", attempt)

						if attempt >= 5 {
							ctx.Logger().Error("event_bus: max reconnection attempts reached, exiting retry loop")
							return
						}
					}
				}
				if strings.Contains(er.Error(), "context canceled") {
					ctx.Logger().Error("event_bus: context cancelled, terminating")
					break
				}
				continue
			}

			var notifyData NotifyData
			var err = json.Unmarshal([]byte(notification.Payload), &notifyData)
			if err != nil {
				ctx.Logger().Error("event_bus: failed to unmarshal notification", "error", err, "payload", notification.Payload)
			}
			p.handleNotify(notifyData)
		}
	}()
	return nil
}

func (p *PostgresEventBus) handleNotify(data NotifyData) {
	event, err := p.repo.ClaimEventForSignal(p.ctx, data.Event, data.Signal)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			p.ctx.Logger().Error("event_bus: failed to resolve event", "error", err, "eventId", data.Event, "signal", data.Signal)
		} else {
			p.ctx.Logger().Info("event_bus: no event found for notification", "eventId", data.Event, "signal", data.Signal)
		}
		return
	}
	p.ctx.Logger().Debug("event_bus: received event", "channel", EVENT_BUS_CHANNEL,
		"signal", data.Signal,
		"eventName", event.EventName,
		"eventType", event.EventType,
		"eventStatus", event.EventStatus)
	eventCtx := p.ctx.WithArgs(&extctx.LoggerArgs{
		TransactionId: event.IllTransactionID,
		EventId:       event.ID,
	})
	switch data.Signal {
	case SignalTaskCreated, SignalNoticeCreated:
		triggerHandlers(eventCtx, event, p.EventCreatedHandlers, data.Signal)
	case SignalTaskBegin:
		triggerHandlers(eventCtx, event, p.TaskStartedHandlers, data.Signal)
	case SignalTaskComplete:
		triggerHandlers(eventCtx, event, p.TaskCompletedHandlers, data.Signal)
	default:
		p.ctx.Logger().Error("event_bus: unsupported signal", "signal", data.Signal, "eventName", event.EventName)
	}
}

func triggerHandlers(ctx extctx.ExtendedContext, event Event, handlersMap map[EventName][]func(ctx extctx.ExtendedContext, event Event), signal Signal) {
	var wg sync.WaitGroup
	handlers, ok := handlersMap[event.EventName]
	if ok {
		ctx.Logger().Debug("event_bus: found handlers for event", "count", len(handlers), "eventName", event.EventName, "signal", signal)
		for _, handler := range handlers {
			wg.Add(1)
			go func(h func(extctx.ExtendedContext, Event), e Event) {
				defer wg.Done()
				h(ctx.WithArgs(&extctx.LoggerArgs{TransactionId: event.IllTransactionID, EventId: event.ID}), e)
			}(handler, event)
		}
	} else {
		ctx.Logger().Debug("event_bus: no handlers found for event", "eventName", event.EventName, "signal", signal)
	}
	wg.Wait() // Wait for all goroutines to finish
	ctx.Logger().Debug("event_bus: all handlers finished", "eventName", event.EventName, "signal", signal)
}

func (p *PostgresEventBus) CreateTask(illTransactionID string, eventName EventName, data EventData, parentId *string) (string, error) {
	id := uuid.New().String()
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
			LastSignal:       "",
		})
		if err != nil && event.ParentID.Valid {
			return err
		}
		err = eventRepo.Notify(p.ctx, id, SignalTaskCreated)
		p.ctx.Logger().Debug("event_bus: created TASK", "eventName", eventName, "eventId", event.ID, "status", event.EventStatus)
		return err
	})
}

func (p *PostgresEventBus) CreateNotice(illTransactionID string, eventName EventName, data EventData, status EventStatus) (string, error) {
	id := uuid.New().String()
	return id, p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		event, err := eventRepo.SaveEvent(p.ctx, SaveEventParams{
			ID:               id,
			IllTransactionID: illTransactionID,
			Timestamp:        getPgNow(),
			EventType:        EventTypeNotice,
			EventName:        eventName,
			EventStatus:      status,
			EventData:        data,
			LastSignal:       "",
		})
		if err != nil {
			return err
		}
		err = eventRepo.Notify(p.ctx, id, SignalNoticeCreated)
		p.ctx.Logger().Debug("event_bus: created NOTICE", "eventName", eventName, "eventId", event.ID, "status", status)
		return err
	})
}

func (p *PostgresEventBus) BeginTask(eventId string) error {
	event, err := p.repo.GetEvent(p.ctx, eventId)
	if err != nil {
		return err
	}
	if event.EventType != EventTypeTask {
		return errors.New("event is not a TASK")
	}
	if event.EventStatus != EventStatusNew {
		return errors.New("event is not in state NEW")
	}
	return p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		err = eventRepo.UpdateEventStatus(p.ctx, UpdateEventStatusParams{
			ID:          eventId,
			EventStatus: EventStatusProcessing,
		})
		if err != nil {
			return err
		}
		err = eventRepo.Notify(p.ctx, eventId, SignalTaskBegin)
		return err
	})
}

func (p *PostgresEventBus) CompleteTask(eventId string, result *EventResult, status EventStatus) error {
	event, err := p.repo.GetEvent(p.ctx, eventId)
	if err != nil {
		return err
	}
	if event.EventType != EventTypeTask {
		return errors.New("event is not a TASK")
	}
	if event.EventStatus != EventStatusProcessing {
		return errors.New("event is not in state PROCESSING")
	}
	event.EventStatus = status
	if result != nil {
		event.ResultData = *result
	}
	return p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		event.LastSignal = "" // Reset notification status before saving
		_, err = eventRepo.SaveEvent(p.ctx, SaveEventParams(event))
		if err != nil {
			return err
		}
		err = eventRepo.Notify(p.ctx, eventId, SignalTaskComplete)
		return err
	})
}

func (p *PostgresEventBus) HandleEventCreated(eventName EventName, f func(ctx extctx.ExtendedContext, event Event)) {
	if p.EventCreatedHandlers == nil {
		p.EventCreatedHandlers = make(map[EventName][]func(ctx extctx.ExtendedContext, event Event))
	}
	if p.EventCreatedHandlers[eventName] == nil {
		p.EventCreatedHandlers[eventName] = []func(ctx extctx.ExtendedContext, event Event){}
	}
	p.EventCreatedHandlers[eventName] = append(p.EventCreatedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleTaskStarted(eventName EventName, f func(ctx extctx.ExtendedContext, event Event)) {
	if p.TaskStartedHandlers == nil {
		p.TaskStartedHandlers = make(map[EventName][]func(ctx extctx.ExtendedContext, event Event))
	}
	if p.TaskStartedHandlers[eventName] == nil {
		p.TaskStartedHandlers[eventName] = []func(ctx extctx.ExtendedContext, event Event){}
	}
	p.TaskStartedHandlers[eventName] = append(p.TaskStartedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleTaskCompleted(eventName EventName, f func(ctx extctx.ExtendedContext, event Event)) {
	if p.TaskCompletedHandlers == nil {
		p.TaskCompletedHandlers = make(map[EventName][]func(ctx extctx.ExtendedContext, event Event))
	}
	if p.TaskCompletedHandlers[eventName] == nil {
		p.TaskCompletedHandlers[eventName] = []func(ctx extctx.ExtendedContext, event Event){}
	}
	p.TaskCompletedHandlers[eventName] = append(p.TaskCompletedHandlers[eventName], f)
}

func (p *PostgresEventBus) ProcessTask(ctx extctx.ExtendedContext, event Event, h func(extctx.ExtendedContext, Event) (EventStatus, *EventResult)) {
	err := p.BeginTask(event.ID)
	if err != nil {
		ctx.Logger().Error("event_bus: failed to start processing TASK event", "error", err, "eventName", event.EventName, "eventStatus", event.EventStatus)
		return
	}

	status, result := h(ctx, event)

	err = p.CompleteTask(event.ID, result, status)
	if err != nil {
		ctx.Logger().Error("event_bus: failed to complete processing TASK event", "error", err, "eventName", event.EventName)
	}
}

func (p *PostgresEventBus) FindAncestor(ctx extctx.ExtendedContext, descendant *Event, eventName EventName) *Event {
	var event *Event
	parentId := getParentId(descendant)
	for {
		if parentId == nil {
			break
		}
		found, err := p.repo.GetEvent(ctx, *parentId)
		if err != nil {
			ctx.Logger().Error("event_bus: failed to get parent event", "eventName", eventName, "parentId", parentId, "error", err)
		} else if found.EventName == eventName {
			event = &found
			break
		} else {
			parentId = getParentId(&found)
		}
	}
	return event
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
