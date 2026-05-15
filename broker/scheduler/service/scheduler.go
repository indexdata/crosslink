package skd_service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	skd_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"
)

var SCHEDULER_RETRY_DELAY, _ = utils.GetEnvAny("SCHEDULER_RETRY_DELAY", time.Duration(5*float64(time.Minute)), func(val string) (time.Duration, error) {
	d, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf("invalid SCHEDULER_RETRY_DELAY value: %s", val)
	}
	return d, nil
})

type SchedulerService struct {
	skdRepo    skd_db.SkdRepo
	eventBus   events.EventBus
	connString string
	// notifyCh is written by Listen and read by schedulerLoop via waitUntil.
	notifyCh chan struct{}
	notify   <-chan struct{}
}

// NewSchedulerService creates a SchedulerService wired to the given repo,
// event bus, and Postgres connection string (used for the LISTEN connection).
func NewSchedulerService(skdRepo skd_db.SkdRepo, eventBus events.EventBus, connString string) *SchedulerService {
	ch := make(chan struct{}, 1)
	return &SchedulerService{
		skdRepo:    skdRepo,
		eventBus:   eventBus,
		connString: connString,
		notifyCh:   ch,
		notify:     ch,
	}
}

// Listen opens a dedicated Postgres connection and listens on scheduler_channel.
// Each notification wakes the scheduler loop. Reconnects with exponential
// backoff on connection loss. Blocks until ctx is cancelled.
func (s *SchedulerService) Listen(ctx common.ExtendedContext) error {
	var conn *pgx.Conn
	var err error

	connectAndListen := func() error {
		conn, err = pgx.Connect(ctx, s.connString)
		if err != nil {
			ctx.Logger().Error("scheduler: unable to connect to database", "error", err)
			return err
		}
		_, err = conn.Exec(ctx, "LISTEN "+skd_db.SchedulerChannel)
		if err != nil {
			ctx.Logger().Error("scheduler: unable to listen to channel", "channel", skd_db.SchedulerChannel, "error", err)
			_ = conn.Close(ctx)
			return err
		}
		ctx.Logger().Info("scheduler: listening on channel", "channel", skd_db.SchedulerChannel)
		return nil
	}

	if err = connectAndListen(); err != nil {
		return err
	}

	go func() {
		defer func() { _ = conn.Close(ctx) }()
		for {
			_, er := conn.WaitForNotification(ctx)
			if er != nil {
				if strings.Contains(er.Error(), "context canceled") {
					ctx.Logger().Info("scheduler: context cancelled, stopping listener")
					return
				}

				ctx.Logger().Warn("scheduler: notification error, reconnecting", "error", er)

				baseDelay := 1 * time.Second
				maxDelay := 30 * time.Second
				delay := baseDelay

				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(delay):
					}
					if err = connectAndListen(); err == nil {
						break
					}
					ctx.Logger().Error("scheduler: reconnect failed", "error", err, "next_try_in", delay)
					delay = time.Duration(float64(delay) * 1.5)
					if delay > maxDelay {
						delay = maxDelay
					}
				}
				continue
			}
			select {
			case s.notifyCh <- struct{}{}:
			default:
			}
		}
	}()

	return nil
}

// Run starts the scheduler loop, blocking until ctx is cancelled.
// Call Listen before Run to enable early wake-up via Postgres notifications.
func (s *SchedulerService) Run(ctx common.ExtendedContext) {
	s.schedulerLoop(ctx)
}

func (s *SchedulerService) schedulerLoop(ctx common.ExtendedContext) {
	for {
		s.runDueTasks(ctx)

		nextRunAt := s.getNextRunAt(ctx)
		if !waitUntil(ctx, nextRunAt, s.notify, SCHEDULER_RETRY_DELAY) {
			return // context cancelled
		}
	}
}

func (s *SchedulerService) runDueTasks(ctx common.ExtendedContext) {
	for {
		task, err := s.skdRepo.ClaimNextScheduledTask(ctx)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				ctx.Logger().Error("failed to claim next scheduled task", "error", err)
			}
			return
		}

		_, err = s.eventBus.CreateTask(events.DEFAULT_ILL_TRANSACTION_ID, task.EventName, task.Payload, events.EventDomainScheduler, nil, events.SignalConsumers)

		if err != nil {
			task.RunAt = pgtype.Timestamptz{Time: time.Now().Add(SCHEDULER_RETRY_DELAY), Valid: true}
			s.unlockAndReschedule(ctx, task)
			continue
		}

		if task.CronExpr != "" {
			next, err := nextCronTime(task.CronExpr)
			if err != nil {
				ctx.Logger().Error("invalid cron expression, disabling task", "error", err, "taskId", task.ID)
				s.disableTask(ctx, task)
				continue
			}
			task.RunAt = next
			s.unlockAndReschedule(ctx, task)
		} else {
			s.disableTask(ctx, task)
		}
	}
}

func (s *SchedulerService) disableTask(ctx common.ExtendedContext, task skd_db.ScheduledTask) {
	task.Status = skd_db.ScheduledTaskStatusStopped
	_, err := s.skdRepo.SaveScheduledTask(ctx, skd_db.SaveScheduledTaskParams(task))
	if err != nil {
		ctx.Logger().Error("failed to update scheduled task", "error", err, "taskId", task.ID)
	}
}

func (s *SchedulerService) unlockAndReschedule(ctx common.ExtendedContext, task skd_db.ScheduledTask) {
	task.Status = skd_db.ScheduledTaskStatusPending
	_, err := s.skdRepo.SaveScheduledTask(ctx, skd_db.SaveScheduledTaskParams(task))
	if err != nil {
		ctx.Logger().Error("failed to reschedule scheduled task", "error", err, "taskId", task.ID)
	}
}

// getNextRunAt returns the run_at timestamp of the earliest pending scheduled
// task, or a zero Timestamptz if no pending tasks exist.
func (s *SchedulerService) getNextRunAt(ctx common.ExtendedContext) pgtype.Timestamptz {
	next, err := s.skdRepo.GetNextRunAt(ctx)
	if err != nil {
		ctx.Logger().Error("failed to get next run", "error", err)
		// No pending tasks or query error — return invalid (zero) value.
		return pgtype.Timestamptz{}
	}
	return next
}

// waitUntil blocks until one of three conditions is met:
//  1. nextRunAt is reached (next scheduled task is due)
//  2. a signal arrives on notifyChanged (schedule table was modified)
//  3. the fallback duration elapses (safety poll)
//
// Returns false if the context was cancelled (caller should stop the loop).
func waitUntil(ctx common.ExtendedContext, nextRunAt pgtype.Timestamptz, notifyChanged <-chan struct{}, fallback time.Duration) bool {
	sleepDuration := fallback
	if nextRunAt.Valid {
		until := time.Until(nextRunAt.Time)
		if until > 0 && until < fallback {
			sleepDuration = until
		} else if until <= 0 {
			return true
		}
	}

	timer := time.NewTimer(sleepDuration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	case <-notifyChanged:
		return true
	}
}

// nextCronTime parses a standard 5-field cron expression and returns the next
// scheduled execution time after now as a pgtype.Timestamptz.
// Returns an error if the expression is invalid.
func nextCronTime(cronExpr string) (pgtype.Timestamptz, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return pgtype.Timestamptz{}, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}
	next := schedule.Next(time.Now())
	return pgtype.Timestamptz{
		Time:  next,
		Valid: true,
	}, nil
}
