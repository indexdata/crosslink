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

type EventBus interface {
	Start(ctx extctx.ExtendedContext) error
	CreateTask(illTransactionID string, eventName EventName, data EventData) error
	CreateNotice(illTransactionID string, eventName EventName, data EventData, status EventStatus) error
	BeginTask(eventId string) error
	//TODO indicate that the task should be retried by setting the status to EventStatusRetry
	//TODO the event bus will use the default retry timeout and count, but the handler can override this
	//TODO by setting the event.RetryAfter and/or event.RetryCount fields
	CompleteTask(eventId string, result *EventResult, status EventStatus) error
	HandleEventCreated(eventName EventName, f func(ctx extctx.ExtendedContext, event Event))
	HandleTaskStarted(eventName EventName, f func(ctx extctx.ExtendedContext, event Event))
	HandleTaskCompleted(eventName EventName, f func(ctx extctx.ExtendedContext, event Event))
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
			ctx.Logger().Error("event bus unable to connect to database", "error", err)
			return err
		}

		_, err = conn.Exec(ctx, "LISTEN crosslink_channel")
		if err != nil {
			ctx.Logger().Error("event bus unable to listen to channel crosslink_channel", "error", err)
			return err
		}

		ctx.Logger().Info("Successfully connected and listening to channel.")
		return nil
	}

	if err = connectAndListen(); err != nil {
		return err
	}

	go func() {
		for {
			notification, er := conn.WaitForNotification(ctx)
			if er != nil {
				ctx.Logger().Error("Unable to receive notification", "error", err)

				if er.Error() == "conn closed" {
					ctx.Logger().Info("Connection closed, attempting to reconnect...")

					for attempt := 1; ; attempt++ {
						time.Sleep(time.Duration(attempt) * time.Second)
						if err = connectAndListen(); err == nil {
							break
						}
						ctx.Logger().Error("Reconnection attempt failed", "error", err, "attempt", attempt)

						if attempt >= 5 {
							ctx.Logger().Error("Max reconnection attempts reached, exiting retry loop.")
							return
						}
					}
				}
				if strings.Contains(er.Error(), "context canceled") {
					ctx.Logger().Error("Context cancelled, stop listening to events")
					break
				}
				continue
			}

			ctx.Logger().Info("Received notification on channel", "channel", notification.Channel, "payload", notification.Payload)
			var notifyData NotifyData
			var err = json.Unmarshal([]byte(notification.Payload), &notifyData)
			if err != nil {
				ctx.Logger().Error("Failed to unmarshal notification", "error", err)
			}
			p.handleNotify(notifyData)
		}
	}()
	return nil
}

func (p *PostgresEventBus) handleNotify(data NotifyData) {
	event, err := p.repo.GetEvent(p.ctx, data.Event)
	if err != nil {
		p.ctx.Logger().Error("Failed to read event", "error", err, "eventId", data.Event)
		return
	}
	eventCtx := p.ctx.WithArgs(&extctx.LoggerArgs{
		TransactionId: event.IllTransactionID,
		EventId:       event.ID,
	})
	switch data.Signal {
	case SignalTaskCreated, SignalNoticeCreated:
		triggerHandlers(eventCtx, event, p.EventCreatedHandlers)
	case SignalTaskBegin:
		triggerHandlers(eventCtx, event, p.TaskStartedHandlers)
	case SignalTaskComplete:
		triggerHandlers(eventCtx, event, p.TaskCompletedHandlers)
	//TODO on SignalTaskRetry handle the delay specified with event.Deadline
	default:
		p.ctx.Logger().Error("Not supported signal", "signal", data.Signal)
	}
}

func triggerHandlers(ctx extctx.ExtendedContext, event Event, handlersMap map[EventName][]func(ctx extctx.ExtendedContext, event Event)) {
	var wg sync.WaitGroup
	handlers, ok := handlersMap[event.EventName]
	if ok {
		for _, handler := range handlers {
			wg.Add(1)
			go func(h func(extctx.ExtendedContext, Event), e Event) {
				defer wg.Done()
				h(ctx.WithArgs(&extctx.LoggerArgs{TransactionId: event.IllTransactionID, EventId: event.ID}), e)
			}(handler, event)
		}
	} else {
		ctx.Logger().Warn("No handlers found for event")
	}
	wg.Wait() // Wait for all goroutines to finish
	ctx.Logger().Info("All handlers finished.")
}

func (p *PostgresEventBus) CreateTask(illTransactionID string, eventName EventName, data EventData) error {
	id := uuid.New().String()
	return p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		_, err := eventRepo.SaveEvent(p.ctx, SaveEventParams{
			ID:               id,
			IllTransactionID: illTransactionID,
			Timestamp:        getNow(),
			EventType:        EventTypeTask,
			EventName:        eventName,
			EventStatus:      EventStatusNew,
			EventData:        data,
		})
		if err != nil {
			return err
		}
		err = eventRepo.Notify(p.ctx, id, SignalTaskCreated)
		return err
	})
}

func (p *PostgresEventBus) CreateNotice(illTransactionID string, eventName EventName, data EventData, status EventStatus) error {
	id := uuid.New().String()
	return p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		_, err := eventRepo.SaveEvent(p.ctx, SaveEventParams{
			ID:               id,
			IllTransactionID: illTransactionID,
			Timestamp:        getNow(),
			EventType:        EventTypeNotice,
			EventName:        eventName,
			EventStatus:      status,
			EventData:        data,
		})
		if err != nil {
			return err
		}
		err = eventRepo.Notify(p.ctx, id, SignalNoticeCreated)
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
	return p.repo.WithTxFunc(p.ctx, func(eventRepo EventRepo) error {
		_, err = eventRepo.SaveEvent(p.ctx, SaveEventParams{
			ID:               event.ID,
			IllTransactionID: event.IllTransactionID,
			Timestamp:        event.Timestamp,
			EventType:        event.EventType,
			EventName:        event.EventName,
			EventStatus:      status,
			EventData:        event.EventData,
			ResultData:       *result,
		})
		if err != nil {
			return err
		}
		err = eventRepo.Notify(p.ctx, eventId, SignalTaskComplete)
		// TODO if event is in status EventStatusRetry, do not notify SignalTaskComplete, instead create a new event with the same data,
		// TODO calculate the event.Deadline and event.RetryNumber based on the default config or event.RetryAfter and event.RetryCount fields if specified
		// TODO then notify SignalTaskRetry
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

func getNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
