package event

import (
	"context"
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
	Start() error
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
func (p *PostgresEventBus) Start() error {
	conn, err := pgx.Connect(context.Background(), p.ConnectionString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "event bus unable to connect to database")
		return err
	}

	_, err = conn.Exec(context.Background(), "LISTEN crosslink_channel")
	if err != nil {
		fmt.Fprintf(os.Stderr, "event bus unable to listen to channel crosslink_channel")
		return err
	}

	// Start a goroutine to receive notifications
	go func() {
		for {
			notification, er := conn.WaitForNotification(context.Background())
			if er != nil {
				fmt.Fprintf(os.Stderr, "Unable to receive notification: %v\n", er)
				if er.Error() == "conn closed" {
					break // TODO I think we should reopen connection here
				}
				continue
			}

			fmt.Printf("Received notification on channel %s: %s\n", notification.Channel, notification.Payload)
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
