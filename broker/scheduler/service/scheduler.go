package sched_service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/teambition/rrule-go"
)

var SCHEDULER_RETRY_DELAY, _ = utils.GetEnvAny("SCHEDULER_RETRY_DELAY", time.Duration(5*float64(time.Minute)), func(val string) (time.Duration, error) {
	d, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf("invalid SCHEDULER_RETRY_DELAY value: %s", val)
	}
	return d, nil
})

type SchedulerService struct {
	schedRepo  sched_db.SchedRepo
	eventBus   events.EventBus
	connString string
	// notifyCh is written by Listen and read by schedulerLoop via waitUntil.
	notifyCh chan struct{}
	notify   <-chan struct{}
	// #nosec G404 - math/rand is sufficient for reconnect jitter
	randGen *rand.Rand
}

// NewSchedulerService creates a SchedulerService wired to the given repo,
// event bus, and Postgres connection string (used for the LISTEN connection).
func NewSchedulerService(schedRepo sched_db.SchedRepo, eventBus events.EventBus, connString string) *SchedulerService {
	ch := make(chan struct{}, 1)
	return &SchedulerService{
		schedRepo:  schedRepo,
		eventBus:   eventBus,
		connString: connString,
		notifyCh:   ch,
		notify:     ch,
		// #nosec G404 - math/rand is sufficient for connection jitter
		randGen: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Listen opens a dedicated Postgres connection and listens on
// sched_db.SchedulerChannel. Each notification wakes the scheduler loop.
// After the initial connection and LISTEN registration succeed, it starts a
// background goroutine to receive notifications and returns. The listener
// reconnects with exponential backoff on connection loss and runs until ctx is
// cancelled.
func (s *SchedulerService) Listen(ctx common.ExtendedContext) error {
	// openConn establishes a fresh connection and registers the LISTEN.
	// The caller is responsible for closing the returned connection.
	openConn := func() (*pgx.Conn, error) {
		c, err := pgx.Connect(ctx, s.connString)
		if err != nil {
			ctx.Logger().Error("scheduler: unable to connect to database", "error", err)
			return nil, err
		}
		if _, err = c.Exec(ctx, "LISTEN "+sched_db.SchedulerChannel); err != nil {
			ctx.Logger().Error("scheduler: unable to listen to channel", "channel", sched_db.SchedulerChannel, "error", err)
			_ = c.Close(ctx)
			return nil, err
		}
		ctx.Logger().Info("scheduler: listening on channel", "channel", sched_db.SchedulerChannel)
		return c, nil
	}

	// Verify we can connect before spawning the goroutine.
	conn, err := openConn()
	if err != nil {
		return err
	}

	go func() {
		// conn is fully local to this goroutine; always close before returning.
		defer func() {
			if conn != nil {
				_ = conn.Close(ctx)
			}
		}()

		for {
			_, er := conn.WaitForNotification(ctx)
			if er != nil {
				if errors.Is(er, context.Canceled) || errors.Is(er, context.DeadlineExceeded) {
					ctx.Logger().Info("scheduler: context cancelled, stopping listener")
					return
				}

				ctx.Logger().Warn("scheduler: notification error, reconnecting", "error", er)

				// Close the broken connection before attempting to reconnect
				// so we don't leak the old socket or its LISTEN registration.
				_ = conn.Close(ctx)
				conn = nil

				baseDelay := 1 * time.Second
				maxDelay := 30 * time.Second
				delay := baseDelay

				for {
					// Add random jitter (0–500 ms) to spread reconnect attempts
					// across multiple instances that lost their connection simultaneously.
					jitter := time.Duration(s.randGen.Intn(500)) * time.Millisecond
					select {
					case <-ctx.Done():
						return
					case <-time.After(delay + jitter):
					}
					newConn, connErr := openConn()
					if connErr == nil {
						conn = newConn
						break
					}
					ctx.Logger().Error("scheduler: reconnect failed", "error", connErr, "next_try_in", delay+jitter)
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
		s.rescheduleLongRunningTasks(ctx)
		madeProgress := s.runDueTasks(ctx)

		nextRunAt := s.getNextRunAt(ctx)
		if !waitUntil(ctx, nextRunAt, s.notify, SCHEDULER_RETRY_DELAY, madeProgress) {
			return // context cancelled
		}
	}
}

// runDueTasks processes all currently claimable tasks.
// Returns true if at least one task was successfully claimed and dispatched.
func (s *SchedulerService) runDueTasks(ctx common.ExtendedContext) bool {
	madeProgress := false
	for {
		err := s.schedRepo.WithTxFunc(ctx, func(txRepo sched_db.SchedRepo) error {
			task, txErr := txRepo.ClaimNextScheduledTask(ctx)
			if txErr != nil {
				return txErr
			}

			// Publish the event. If this fails the transaction rolls back,
			// the claim is undone, and the task stays 'pending' for the next cycle.
			_, txErr = s.eventBus.CreateTask(events.DEFAULT_ILL_TRANSACTION_ID, task.EventName, task.ActionData, events.EventDomainScheduler, nil, events.SignalConsumers)
			if txErr != nil {
				return txErr
			}

			// Compute and persist the task's next state.
			if task.Schedule != "" {
				next, innerErr := NextScheduleTime(task.Schedule)
				if innerErr != nil {
					ctx.Logger().Error("invalid rrule string, disabling task", "error", innerErr, "taskId", task.ID)
					task.Status = sched_db.ScheduledTaskStatusStopped
					task.RunAt = pgtype.Timestamptz{Valid: false}
				} else {
					task.RunAt = next
					task.Status = sched_db.ScheduledTaskStatusPending
				}
			} else {
				task.Status = sched_db.ScheduledTaskStatusStopped
				task.RunAt = pgtype.Timestamptz{Valid: false}
			}
			_, txErr = txRepo.SaveScheduledTask(ctx, sched_db.SaveScheduledTaskParams(task))
			return txErr
		})

		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				ctx.Logger().Error("failed to process scheduled task", "error", err)
			}
			return madeProgress
		}
		madeProgress = true
	}
}

// getNextRunAt returns the run_at timestamp of the earliest pending scheduled
// task, or a zero Timestamptz if no pending tasks exist.
func (s *SchedulerService) getNextRunAt(ctx common.ExtendedContext) pgtype.Timestamptz {
	next, err := s.schedRepo.GetNextRunAt(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			ctx.Logger().Error("failed to get next run", "error", err)
		}
		// No pending tasks or query error — return invalid (zero) value.
		return pgtype.Timestamptz{}
	}
	return next
}

// waitUntil blocks until one of three conditions is met:
//  1. nextRunAt is reached (next scheduled task is due). An overdue nextRunAt
//     only causes an immediate return when madeProgress is true — i.e. the
//     previous runDueTasks call actually claimed a task. This prevents a tight
//     spin loop when ClaimNextScheduledTask keeps returning a persistent error
//     while GetNextRunAt still reports an overdue timestamp.
//  2. a signal arrives on notifyChanged (schedule table was modified)
//  3. the fallback duration elapses (safety poll)
//
// Returns false if the context was cancelled (caller should stop the loop).
func waitUntil(ctx common.ExtendedContext, nextRunAt pgtype.Timestamptz, notifyChanged <-chan struct{}, fallback time.Duration, madeProgress bool) bool {
	sleepDuration := fallback
	if nextRunAt.Valid {
		until := time.Until(nextRunAt.Time)
		if until <= 0 && madeProgress {
			// Overdue and we just successfully processed tasks — more may be ready.
			return true
		} else if until > 0 && until < fallback {
			sleepDuration = until
		}
		// If overdue but no progress was made (persistent claim errors), fall
		// through to sleep the full fallback to avoid a CPU-spinning tight loop.
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

// NextScheduleTime parses an RRULE expression and returns the next
// scheduled execution time strictly after now as a pgtype.Timestamptz.
// Returns an error if the expression is invalid.
func NextScheduleTime(schedule string) (pgtype.Timestamptz, error) {
	return nextScheduleTimeAt(schedule, time.Now().UTC())
}

func nextScheduleTimeAt(schedule string, now time.Time) (pgtype.Timestamptz, error) {
	now = now.UTC()

	options, err := rrule.StrToROption(schedule)
	if err != nil {
		return pgtype.Timestamptz{}, fmt.Errorf("invalid rrule string %q: %w", schedule, err)
	}
	// Anchor at today's UTC midnight so omitted time components inherit zero and
	// interval rules have a deterministic daily phase.
	options.Dtstart = now.Truncate(24 * time.Hour)

	rule, err := rrule.NewRRule(*options)
	if err != nil {
		return pgtype.Timestamptz{}, fmt.Errorf("invalid rrule options %q: %w", schedule, err)
	}

	next := rule.After(now, false)
	if next.IsZero() {
		return pgtype.Timestamptz{}, fmt.Errorf("no future occurrences derived from RRULE")
	}

	return pgtype.Timestamptz{
		Time:  next.UTC(),
		Valid: true,
	}, nil
}

// rescheduleLongRunningTasks finds tasks that have been in 'running' state for
// longer than an hour (indicating a crashed or lost worker) and
// resets them to 'pending' so they are picked up again on the next loop tick.
func (s *SchedulerService) rescheduleLongRunningTasks(ctx common.ExtendedContext) {
	const stuckAfter = time.Hour
	tasks, err := s.schedRepo.GetStuckRunningTasks(ctx, stuckAfter)
	if err != nil {
		ctx.Logger().Error("failed to query stuck running tasks", "error", err)
		return
	}
	stuckBefore := time.Now().Add(-stuckAfter)
	for _, candidate := range tasks {
		err = s.schedRepo.WithTxFunc(ctx, func(repo sched_db.SchedRepo) error {
			task, inErr := repo.GetScheduledTaskByIdForUpdate(ctx, candidate.ID, nil)
			if errors.Is(inErr, pgx.ErrNoRows) {
				return nil
			}
			if inErr != nil {
				return inErr
			}
			if task.Status != sched_db.ScheduledTaskStatusRunning ||
				!task.UpdatedAt.Valid ||
				task.UpdatedAt.Time.After(stuckBefore) {
				return nil
			}

			ctx.Logger().Info("rescheduling stuck task", "taskId", task.ID, "eventName", task.EventName)
			if task.Schedule != "" {
				next, nextErr := NextScheduleTime(task.Schedule)
				if nextErr != nil {
					ctx.Logger().Error("invalid rrule string, disabling task", "error", nextErr, "taskId", task.ID)
					task.Status = sched_db.ScheduledTaskStatusStopped
					task.RunAt = pgtype.Timestamptz{Valid: false}
				} else {
					task.Status = sched_db.ScheduledTaskStatusPending
					task.RunAt = next
				}
			} else {
				task.Status = sched_db.ScheduledTaskStatusPending
				task.RunAt = pgtype.Timestamptz{
					Time:  time.Now().Add(SCHEDULER_RETRY_DELAY),
					Valid: true,
				}
			}
			_, inErr = repo.SaveScheduledTask(ctx, sched_db.SaveScheduledTaskParams(task))
			return inErr
		})
		if err != nil {
			ctx.Logger().Error("failed to recover stuck running task", "error", err, "taskId", candidate.ID)
		}
	}
}
