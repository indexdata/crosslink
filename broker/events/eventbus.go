package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type EventBus interface {
	Start(ctx context.Context) error
	CreateTask(illTransactionID string, eventName EventName, data EventData) error
	CreateNotice(illTransactionID string, eventName EventName, data EventData, status EventStatus) error
	BeginTask(eventId string) error
	CompleteTask(eventId string, result *EventResult, status EventStatus) error
	HandleEventCreated(eventName EventName, f func(event Event))
	HandleTaskStarted(eventName EventName, f func(event Event))
	HandleTaskCompleted(eventName EventName, f func(event Event))
}

type PostgresEventBus struct {
	repo                  EventRepo
	ConnectionString      string
	EventCreatedHandlers  map[EventName][]func(event Event)
	TaskStartedHandlers   map[EventName][]func(event Event)
	TaskCompletedHandlers map[EventName][]func(event Event)
}

func NewPostgresEventBus(repo EventRepo, connString string) *PostgresEventBus {
	return &PostgresEventBus{
		repo:             repo,
		ConnectionString: connString,
	}
}

func (p *PostgresEventBus) Start(ctx context.Context) error {
	var conn *pgx.Conn
	var err error

	connectAndListen := func() error {
		conn, err = pgx.Connect(ctx, p.ConnectionString)
		if err != nil {
			fmt.Fprintf(os.Stderr, "event bus unable to connect to database: %v\n", err)
			return err
		}

		_, err = conn.Exec(ctx, "LISTEN crosslink_channel")
		if err != nil {
			fmt.Fprintf(os.Stderr, "event bus unable to listen to channel crosslink_channel: %v\n", err)
			return err
		}

		fmt.Println("Successfully connected and listening to channel.")
		return nil
	}

	if err = connectAndListen(); err != nil {
		return err
	}

	go func() {
		for {
			notification, er := conn.WaitForNotification(ctx)
			if er != nil {
				fmt.Fprintf(os.Stderr, "Unable to receive notification: %v\n", er)

				if er.Error() == "conn closed" {
					fmt.Println("Connection closed, attempting to reconnect...")

					for attempt := 1; ; attempt++ {
						time.Sleep(time.Duration(attempt) * time.Second)
						if err = connectAndListen(); err == nil {
							break
						}

						fmt.Fprintf(os.Stderr, "Reconnection attempt %d failed: %v\n", attempt, err)
						if attempt >= 5 {
							fmt.Println("Max reconnection attempts reached, exiting retry loop.")
							return
						}
					}
				}
				if strings.Contains(er.Error(), "context canceled") {
					fmt.Println("Context cancelled, stop listening to events")
					break
				}
				continue
			}

			fmt.Printf("Received notification on channel %s: %s\n", notification.Channel, notification.Payload)
			var notifyData NotifyData
			var err = json.Unmarshal([]byte(notification.Payload), &notifyData)
			if err != nil {
				fmt.Println("Failed to unmarshal notification")
			}
			p.handleNotify(notifyData)
		}
	}()
	return nil
}

func (p *PostgresEventBus) handleNotify(data NotifyData) {
	event, err := p.repo.GetEvent(data.Event)
	if err != nil {
		fmt.Printf("Failed to read event %s, %s", data.Event, err)
		fmt.Println()
		return
	}
	switch data.Signal {
	case SignalTaskCreated, SignalNoticeCreated:
		triggerHandlers(event, p.EventCreatedHandlers)
	case SignalTaskBegin:
		triggerHandlers(event, p.TaskStartedHandlers)
	case SignalTaskComplete:
		triggerHandlers(event, p.TaskCompletedHandlers)
	default:
		fmt.Printf("Not supported signal %s", data.Signal)
		fmt.Println()
	}
}

func triggerHandlers(event Event, handlersMap map[EventName][]func(event Event)) {
	var wg sync.WaitGroup
	handlers, ok := handlersMap[event.EventName]
	if ok {
		for _, handler := range handlers {
			wg.Add(1)
			go func(h func(Event), e Event) {
				defer wg.Done()
				h(e)
			}(handler, event)
		}
	} else {
		fmt.Printf("No handlers found for event: %s", event.EventName)
		fmt.Println()
	}
	wg.Wait() // Wait for all goroutines to finish
	fmt.Println("All handlers finished.")
}

func (p *PostgresEventBus) CreateTask(illTransactionID string, eventName EventName, data EventData) error {
	id := uuid.New().String()
	return p.repo.WithTxFunc(context.Background(), func(eventRepo EventRepo) error {
		_, err := eventRepo.SaveEvent(SaveEventParams{
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
		err = eventRepo.Notify(id, SignalTaskCreated)
		return err
	})
}

func (p *PostgresEventBus) CreateNotice(illTransactionID string, eventName EventName, data EventData, status EventStatus) error {
	id := uuid.New().String()
	return p.repo.WithTxFunc(context.Background(), func(eventRepo EventRepo) error {
		_, err := eventRepo.SaveEvent(SaveEventParams{
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
		err = eventRepo.Notify(id, SignalNoticeCreated)
		return err
	})
}

func (p *PostgresEventBus) BeginTask(eventId string) error {
	event, err := p.repo.GetEvent(eventId)
	if err != nil {
		return err
	}
	if event.EventType != EventTypeTask {
		return errors.New("event is not a TASK")
	}
	if event.EventStatus != EventStatusNew {
		return errors.New("event is not in state NEW")
	}
	return p.repo.WithTxFunc(context.Background(), func(eventRepo EventRepo) error {
		err = eventRepo.UpdateEventStatus(UpdateEventStatusParams{
			ID:          eventId,
			EventStatus: EventStatusProcessing,
		})
		if err != nil {
			return err
		}
		err = eventRepo.Notify(eventId, SignalTaskBegin)
		return err
	})
}

func (p *PostgresEventBus) CompleteTask(eventId string, result *EventResult, status EventStatus) error {
	event, err := p.repo.GetEvent(eventId)
	if err != nil {
		return err
	}
	if event.EventType != EventTypeTask {
		return errors.New("event is not a TASK")
	}
	if event.EventStatus != EventStatusProcessing {
		return errors.New("event is not in state PROCESSING")
	}
	return p.repo.WithTxFunc(context.Background(), func(eventRepo EventRepo) error {
		_, err = eventRepo.SaveEvent(SaveEventParams{
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
		err = eventRepo.Notify(eventId, SignalTaskComplete)
		return err
	})
}

func (p *PostgresEventBus) HandleEventCreated(eventName EventName, f func(event Event)) {
	if p.EventCreatedHandlers == nil {
		p.EventCreatedHandlers = make(map[EventName][]func(event Event))
	}
	if p.EventCreatedHandlers[eventName] == nil {
		p.EventCreatedHandlers[eventName] = []func(event Event){}
	}
	p.EventCreatedHandlers[eventName] = append(p.EventCreatedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleTaskStarted(eventName EventName, f func(event Event)) {
	if p.TaskStartedHandlers == nil {
		p.TaskStartedHandlers = make(map[EventName][]func(event Event))
	}
	if p.TaskStartedHandlers[eventName] == nil {
		p.TaskStartedHandlers[eventName] = []func(event Event){}
	}
	p.TaskStartedHandlers[eventName] = append(p.TaskStartedHandlers[eventName], f)
}

func (p *PostgresEventBus) HandleTaskCompleted(eventName EventName, f func(event Event)) {
	if p.TaskCompletedHandlers == nil {
		p.TaskCompletedHandlers = make(map[EventName][]func(event Event))
	}
	if p.TaskCompletedHandlers[eventName] == nil {
		p.TaskCompletedHandlers[eventName] = []func(event Event){}
	}
	p.TaskCompletedHandlers[eventName] = append(p.TaskCompletedHandlers[eventName], f)
}

func getNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
