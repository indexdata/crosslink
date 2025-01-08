package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	repository "github.com/indexdata/crosslink/broker/db"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/indexdata/crosslink/broker/db/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"os"
	"time"
)

type EventBus interface {
	Start(ctx context.Context) error
	CreateTask(illTransactionID string, eventName model.EventName, data model.EventData) error
	CreateNotice(illTransactionID string, eventName model.EventName, data model.EventData, status model.EventStatus) error
	BeginTask(eventId string) error
	CompleteTask(eventId string, result *model.EventResult, status model.EventStatus) error
	//HandleEventCreated(eventName model.EventName, f func(event queries.Event)) error
	//HandleTaskStarted(eventName model.EventName, f func(event queries.Event)) error
	//HandleTaskCompleted(eventName model.EventName, f func(event queries.Event)) error
}

type PostgresEventBus struct {
	Repository       repository.Repository
	ConnectionString string
}

func NewPostgresEventBus(repo repository.Repository, connString string) *PostgresEventBus {
	return &PostgresEventBus{
		Repository:       repo,
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

				continue
			}

			fmt.Printf("Received notification on channel %s: %s\n", notification.Channel, notification.Payload)
			var notifyData model.NotifyData
			var err = json.Unmarshal([]byte(notification.Payload), &notifyData)
			if err != nil {
				fmt.Println("Failed to unmarshal notification")
			}
		}
	}()

	return nil
}

func (p *PostgresEventBus) CreateTask(illTransactionID string, eventName model.EventName, data model.EventData) error {
	id := uuid.New().String()
	return p.Repository.WithTx(context.Background(), func(repo repository.Repository) error {
		_, err := repo.SaveEvent(queries.SaveEventParams{
			ID:               id,
			IllTransactionID: illTransactionID,
			Timestamp:        getNow(),
			EventType:        model.EventTypeTask,
			EventName:        eventName,
			EventStatus:      model.EventStatusNew,
			EventData:        data,
		})
		if err != nil {
			return err
		}
		err = repo.Notify(id, model.SignalTaskCreated)
		return err
	})
}

func (p *PostgresEventBus) CreateNotice(illTransactionID string, eventName model.EventName, data model.EventData, status model.EventStatus) error {
	id := uuid.New().String()
	return p.Repository.WithTx(context.Background(), func(repo repository.Repository) error {
		_, err := repo.SaveEvent(queries.SaveEventParams{
			ID:               id,
			IllTransactionID: illTransactionID,
			Timestamp:        getNow(),
			EventType:        model.EventTypeNotice,
			EventName:        eventName,
			EventStatus:      status,
			EventData:        data,
		})
		if err != nil {
			return err
		}
		err = repo.Notify(id, model.SignalNoticeCreated)
		return err
	})
}

func (p *PostgresEventBus) BeginTask(eventId string) error {
	event, err := p.Repository.GetEvent(eventId)
	if err != nil {
		return err
	}
	if event.EventType != model.EventTypeTask {
		return errors.New("event is not a TASK")
	}
	if event.EventStatus != model.EventStatusNew {
		return errors.New("event is not in state NEW")
	}
	return p.Repository.WithTx(context.Background(), func(repo repository.Repository) error {
		err = repo.UpdateEventStatus(queries.UpdateEventStatusParams{
			ID:          eventId,
			EventStatus: model.EventStatusProcessing,
		})
		if err != nil {
			return err
		}
		err = repo.Notify(eventId, model.SignalTaskBegin)
		return err
	})
}

func (p *PostgresEventBus) CompleteTask(eventId string, result *model.EventResult, status model.EventStatus) error {
	event, err := p.Repository.GetEvent(eventId)
	if err != nil {
		return err
	}
	if event.EventType != model.EventTypeTask {
		return errors.New("event is not a TASK")
	}
	if event.EventStatus != model.EventStatusProcessing {
		return errors.New("event is not in state PROCESSING")
	}
	return p.Repository.WithTx(context.Background(), func(repo repository.Repository) error {
		_, err = repo.SaveEvent(queries.SaveEventParams{
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
		err = repo.Notify(eventId, model.SignalTaskComplete)
		return err
	})
}

func getNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
