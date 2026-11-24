package sched_service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/email"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/repo"
	schedapi "github.com/indexdata/crosslink/broker/scheduler/api"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	schedservice "github.com/indexdata/crosslink/broker/scheduler/service"
	"github.com/indexdata/crosslink/broker/tenant"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type batchDeliverySMTP func() error

func (batchDeliverySMTP) IsReadyToSend() bool                           { return true }
func (send batchDeliverySMTP) SendEmail(string, []string, []byte) error { return send() }

type batchDeliveryPDF struct{}

func (batchDeliveryPDF) GeneratePdfPullSlipForPrs(common.ExtendedContext, []pr_db.PatronRequest) ([]byte, error) {
	return []byte("test pull slip"), nil
}

type batchDeliveryCommitAcknowledgement struct {
	sched_db.BatchEmailDeliveryRepo
	afterCommit func() error
}

type batchDeliveryWireSMTP struct {
	address string
	sends   atomic.Int32
}

func (*batchDeliveryWireSMTP) IsReadyToSend() bool { return true }

func (s *batchDeliveryWireSMTP) SendEmail(from string, to []string, raw []byte) error {
	s.sends.Add(1)
	return (&email.DefaultMailer{}).SendMail(s.address, nil, from, to, raw)
}

type batchDeliveryAcceptanceResult struct {
	sched_db.BatchEmailDeliveryRepo
	result chan error
}

func (r batchDeliveryAcceptanceResult) RecordBatchEmailAcceptance(ctx common.ExtendedContext, runID string) error {
	err := r.BatchEmailDeliveryRepo.RecordBatchEmailAcceptance(ctx, runID)
	r.result <- err
	return err
}

func (r batchDeliveryCommitAcknowledgement) RecordBatchEmailAcceptance(ctx common.ExtendedContext, runID string) error {
	if err := r.BatchEmailDeliveryRepo.RecordBatchEmailAcceptance(ctx, runID); err != nil {
		return err
	}
	return r.afterCommit()
}

type batchEmailFixture struct {
	prRepo  pr_db.PrRepo
	illRepo ill_db.IllRepo
	task    sched_db.ScheduledTask
	event   events.Event
	request pr_db.PatronRequest
}

func queueBatchEmail(t *testing.T, eventRepo *events.PgEventRepo) batchEmailFixture {
	t.Helper()
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	prRepo := pr_db.CreatePrRepo(pool, false)
	illRepo := ill_db.CreateIllRepo(pool)

	now := pgtype.Timestamp{Time: time.Now(), Valid: true}
	owner := "ISIL:" + uuid.NewString()
	from := "pullslips@example.com"
	peer, err := illRepo.SavePeer(appCtx, ill_db.SavePeerParams{
		ID: uuid.NewString(), Name: owner, RefreshPolicy: ill_db.RefreshPolicyNever,
		RefreshTime: now, CustomData: dirapi.Entry{FromEmail: &from},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, illRepo.DeleteSymbolByPeerId(appCtx, peer.ID))
		require.NoError(t, illRepo.DeletePeer(appCtx, peer.ID))
	})
	_, err = illRepo.SaveSymbol(appCtx, ill_db.SaveSymbolParams{PeerID: peer.ID, SymbolValue: owner})
	require.NoError(t, err)
	template, err := prRepo.SaveTemplate(appCtx, pr_db.SaveTemplateParams{
		ID: uuid.NewString(), Owner: owner, Title: "Pull slips", Purpose: "email", ContentType: "text",
		Subject: pgtype.Text{String: "Pull slips", Valid: true}, Body: "Pull slips are attached",
		Labels: []string{"pullslip-email"}, Audience: pgtype.Text{String: "staff", Valid: true}, CreatedAt: now,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prRepo.DeleteTemplateByIdAndOwner(appCtx, template.ID, owner)) })
	pr, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: uuid.NewString(), CreatedAt: now, Language: "english", Items: []pr_db.PrItem{},
		Side: prservice.SideLending, State: prservice.LenderStateWillSupply,
		SupplierSymbol: pgtype.Text{String: owner, Valid: true},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prRepo.DeletePatronRequest(appCtx, pr.ID)) })
	task := saveBatchRunTask(t, owner, schedoapi.EmailPullslips, false)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
	runs, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	return batchEmailFixture{prRepo: prRepo, illRepo: illRepo, task: task, event: runs[0], request: pr}
}

func TestBatchEmailPersistsDeliveryProgress(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		smtpError              error
		loseCommitConfirmation bool
		replayDelivery         bool
		changeState            bool
		wantStatus             events.EventStatus
	}{
		{name: "accepted", wantStatus: events.EventStatusSuccess},
		{name: "resume accepted delivery", replayDelivery: true, wantStatus: events.EventStatusSuccess},
		{name: "resume after request state changes", replayDelivery: true, changeState: true, wantStatus: events.EventStatusSuccess},
		{name: "acceptance commit confirmation lost", loseCommitConfirmation: true, wantStatus: events.EventStatusSuccess},
		{name: "unconfirmed", replayDelivery: true, smtpError: errors.New("SMTP connection lost"), wantStatus: events.EventStatusError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := schedRepo.(*sched_db.PgSchedRepo).Pool
			eventRepo := app.CreateEventRepo(pool).(*events.PgEventRepo)
			fixture := queueBatchEmail(t, eventRepo)
			runID := fixture.event.ID

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bus := events.NewPostgresEventBus(eventRepo, connString)
			require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
			event, attempt, err := eventRepo.ClaimEventAttemptForSignal(appCtx, runID, events.SignalTaskCreated)
			require.NoError(t, err)
			workCtx := repo.WithBatchAttempt(appCtx, attempt)
			require.ErrorIs(t, schedRepo.RecordBatchEmailIntent(appCtx, runID, []string{fixture.request.ID}, true), repo.ErrBatchAttemptLost)

			sendCount := 0
			var startedAt, acceptedAt pgtype.Timestamptz
			var selectedIDs []string
			var queuePrintedActions bool
			mailer := batchDeliverySMTP(func() error {
				sendCount++
				require.NoError(t, pool.QueryRow(appCtx, `SELECT selected_request_ids, queue_printed_actions, started_at, accepted_at
					FROM batch_action_email_delivery WHERE run_id = $1`, runID).
					Scan(&selectedIDs, &queuePrintedActions, &startedAt, &acceptedAt))
				require.Equal(t, []string{fixture.request.ID}, selectedIDs)
				require.True(t, queuePrintedActions)
				require.True(t, startedAt.Valid)
				require.False(t, acceptedAt.Valid)
				require.Error(t, schedRepo.RecordBatchEmailIntent(workCtx, runID, []string{"different-request"}, false))
				children, err := eventRepo.GetPatronRequestEvents(appCtx, fixture.request.ID)
				require.NoError(t, err)
				require.Empty(t, children)
				return tc.smtpError
			})
			var deliveryRepo sched_db.BatchEmailDeliveryRepo = schedRepo
			var firstAcceptance pgtype.Timestamptz
			var acceptanceWrites int
			if tc.loseCommitConfirmation {
				deliveryRepo = batchDeliveryCommitAcknowledgement{BatchEmailDeliveryRepo: schedRepo, afterCommit: func() error {
					acceptanceWrites++
					var timestamp pgtype.Timestamptz
					require.NoError(t, pool.QueryRow(appCtx, "SELECT accepted_at FROM batch_action_email_delivery WHERE run_id = $1", runID).Scan(&timestamp))
					require.True(t, timestamp.Valid)
					children, err := eventRepo.GetPatronRequestEvents(appCtx, fixture.request.ID)
					require.NoError(t, err)
					require.Empty(t, children)
					if acceptanceWrites == 1 {
						firstAcceptance = timestamp
						return io.ErrUnexpectedEOF
					}
					require.Equal(t, firstAcceptance, timestamp)
					return nil
				}}
			}
			emailSender := schedservice.EmailSenderServiceWithClient(fixture.prRepo, fixture.illRepo, mailer, batchDeliveryPDF{}, bus, deliveryRepo)
			batch := schedservice.NewBatchActionService(bus, fixture.prRepo, schedRepo, emailSender)
			if tc.replayDelivery {
				_, err := bus.ProcessTask(workCtx, event, events.SignalObservers,
					func(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
						event.EventData.BatchActionData.Selector = "id==" + fixture.request.ID
						status, result := emailSender.EmailPullslip(ctx, event)
						require.Equal(t, tc.wantStatus, status, "%+v", result)
						if tc.changeState {
							children, err := eventRepo.GetPatronRequestEvents(appCtx, fixture.request.ID)
							require.NoError(t, err)
							require.Len(t, children, 1)
							child := children[0]
							require.Equal(t, events.EventStatusNew, child.EventStatus)
							require.Equal(t, string(events.SignalTaskCreated), child.LastSignal)
							updated := pr_db.UpdatePatronRequestParams(fixture.request)
							updated.State = prservice.LenderStateShipped
							_, err = fixture.prRepo.UpdatePatronRequest(appCtx, updated)
							require.NoError(t, err)

							// This listener missed the first creation signal.
							listener, err := pgx.Connect(appCtx, connString)
							require.NoError(t, err)
							defer listener.Close(appCtx)
							_, err = listener.Exec(appCtx, "LISTEN "+events.EVENT_BUS_CHANNEL)
							require.NoError(t, err)
							status, result = emailSender.EmailPullslip(ctx, event)
							require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
							waitCtx, cancel := context.WithTimeout(appCtx, 3*time.Second)
							defer cancel()
							notification, err := listener.WaitForNotification(waitCtx)
							require.NoError(t, err)
							var signal events.NotifyData
							require.NoError(t, json.Unmarshal([]byte(notification.Payload), &signal))
							require.Equal(t, events.NotifyData{Event: child.ID, Signal: events.SignalTaskCreated, Target: events.SignalConsumers}, signal)
							return status, result
						}
						return emailSender.EmailPullslip(ctx, event)
					})
				require.NoError(t, err)
			} else {
				batch.BatchAction(workCtx, event)
			}
			require.Equal(t, 1, sendCount)
			if tc.loseCommitConfirmation {
				require.Equal(t, 2, acceptanceWrites)
			}

			stored, err := eventRepo.GetEvent(appCtx, runID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, stored.EventStatus, "%+v", stored.ResultData.EventError)
			require.NoError(t, pool.QueryRow(appCtx, "SELECT selected_request_ids, accepted_at FROM batch_action_email_delivery WHERE run_id = $1", runID).
				Scan(&selectedIDs, &acceptedAt))
			require.Equal(t, []string{fixture.request.ID}, selectedIDs)
			children, err := eventRepo.GetPatronRequestEvents(appCtx, fixture.request.ID)
			require.NoError(t, err)
			if tc.smtpError == nil {
				require.True(t, acceptedAt.Valid)
				require.Len(t, children, 1)
				require.True(t, children[0].ParentID.Valid)
				require.Equal(t, runID, children[0].ParentID.String)
				require.Equal(t, prservice.LenderActionPullslipPrinted, *children[0].EventData.Action)
			} else {
				require.False(t, acceptedAt.Valid)
				require.Empty(t, children)
			}
			require.ErrorIs(t, schedRepo.RecordBatchEmailAcceptance(workCtx, runID), repo.ErrBatchAttemptLost)

			require.NoError(t, schedRepo.DeleteBatchActionEvents(appCtx, fixture.task.ID))
			err = pool.QueryRow(appCtx, "SELECT accepted_at FROM batch_action_email_delivery WHERE run_id = $1", runID).Scan(&acceptedAt)
			require.ErrorIs(t, err, pgx.ErrNoRows)
		})
	}
}

func TestBatchRecoveryFencesWorkerAfterSessionLossDuringSMTP(t *testing.T) {
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool).(*events.PgEventRepo)
	fixture := queueBatchEmail(t, eventRepo)
	runID := fixture.event.ID
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	workerCtx := common.CreateExtCtxWithArgs(ctx, nil)

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer listener.Close()
	require.NoError(t, listener.SetDeadline(time.Now().Add(15*time.Second)))
	inFlight := make(chan struct{})
	acceptData := make(chan struct{})
	smtpResult := make(chan error, 1)
	go func() {
		smtpResult <- receiveBatchSMTP(ctx, listener, inFlight, acceptData)
	}()
	mailer := &batchDeliveryWireSMTP{address: listener.Addr().String()}

	name := "smtp_worker_" + uuid.NewString()
	first := events.NewPostgresEventBus(eventRepo, batchTestBusDSN(t, name))
	acceptance := batchDeliveryAcceptanceResult{BatchEmailDeliveryRepo: schedRepo, result: make(chan error, 1)}
	completion := make(chan error, 1)
	sender := schedservice.EmailSenderServiceWithClient(fixture.prRepo, fixture.illRepo, mailer, batchDeliveryPDF{}, first, acceptance)
	first.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		if event.ID == runID {
			_, err := first.ProcessTask(ctx, event, events.SignalObservers, sender.EmailPullslip)
			completion <- err
		}
	})
	require.NoError(t, first.Start(workerCtx))
	select {
	case <-inFlight:
	case err := <-smtpResult:
		t.Fatalf("SMTP failed before reading DATA: %v", err)
	case err := <-completion:
		t.Fatalf("worker finished before sending SMTP DATA: %v", err)
	case <-ctx.Done():
		t.Fatal("worker did not send SMTP DATA")
	}

	delivery, err := schedRepo.GetBatchEmailDelivery(workerCtx, runID)
	require.NoError(t, err)
	require.Equal(t, []string{fixture.request.ID}, delivery.SelectedRequestIds)
	require.True(t, delivery.QueuePrintedActions)
	require.False(t, delivery.AcceptedAt.Valid)
	var pid int32
	require.NoError(t, pool.QueryRow(workerCtx, `
		SELECT activity.pid
		FROM pg_stat_activity activity
		JOIN pg_locks locks ON locks.pid = activity.pid AND locks.locktype = 'advisory' AND locks.granted
		WHERE activity.application_name = $1
	`, name).Scan(&pid))
	var terminated bool
	require.NoError(t, pool.QueryRow(workerCtx, "SELECT pg_terminate_backend($1, 5000)", pid).Scan(&terminated))
	require.True(t, terminated, "terminate only the first worker's advisory-lock session")

	secondPool, err := pgxpool.New(workerCtx, connString)
	require.NoError(t, err)
	defer secondPool.Close()
	recoveryCtx, stopRecovery := context.WithCancel(ctx)
	defer stopRecovery()
	second := events.NewPostgresEventBus(app.CreateEventRepo(secondPool), connString)
	replaySender := schedservice.EmailSenderServiceWithClient(pr_db.CreatePrRepo(secondPool, false), ill_db.CreateIllRepo(secondPool),
		mailer, batchDeliveryPDF{}, second, sched_db.CreateSchedRepo(secondPool))
	var replays atomic.Int32
	second.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		if event.ID == runID {
			replays.Add(1)
			_, _ = second.ProcessTask(ctx, event, events.SignalObservers, replaySender.EmailPullslip)
		}
	})
	require.NoError(t, second.Start(common.CreateExtCtxWithArgs(recoveryCtx, nil)))
	require.Eventually(t, func() bool {
		var blocked bool
		err := pool.QueryRow(workerCtx, `
			SELECT recovery_blocked_at IS NOT NULL AND recovery_reason = 'SMTP acceptance is unconfirmed' AND attempt_id IS NULL
			FROM batch_action_run_event WHERE event_id = $1
		`, runID).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 20*time.Millisecond)

	// SMTP now accepts the message. The first worker is still running, but
	// the second broker has already fenced its attempt.
	close(acceptData)
	for _, check := range []struct {
		name string
		ch   <-chan error
		want error
	}{
		{"SMTP final DATA response", smtpResult, nil},
		{"acceptance write", acceptance.result, repo.ErrBatchAttemptLost},
		{"task completion", completion, repo.ErrBatchAttemptLost},
	} {
		select {
		case err := <-check.ch:
			require.ErrorIs(t, err, check.want, check.name)
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", check.name)
		}
	}

	require.EqualValues(t, 1, mailer.sends.Load())
	require.Zero(t, replays.Load())
	delivery, err = schedRepo.GetBatchEmailDelivery(workerCtx, runID)
	require.NoError(t, err)
	require.False(t, delivery.AcceptedAt.Valid)
	children, err := eventRepo.GetPatronRequestEvents(workerCtx, fixture.request.ID)
	require.NoError(t, err)
	require.Empty(t, children)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusConflict, requestBatchRun(handler, fixture.task.ID).Code)
}

// receiveBatchSMTP reads the message and waits for the test to release the final DATA response.
func receiveBatchSMTP(ctx context.Context, listener *net.TCPListener, inFlight chan<- struct{}, acceptData <-chan struct{}) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	smtp := textproto.NewConn(conn)
	if err := smtp.PrintfLine("220 localhost"); err != nil {
		return err
	}
	for _, step := range []struct{ command, reply string }{
		{"EHLO ", "250 localhost"},
		{"MAIL FROM:", "250 Sender OK"},
		{"RCPT TO:", "250 Recipient OK"},
		{"DATA", "354 Send message"},
	} {
		line, err := smtp.ReadLine()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(line, step.command) {
			return fmt.Errorf("expected SMTP command %q, got %q", step.command, line)
		}
		if err := smtp.PrintfLine("%s", step.reply); err != nil {
			return err
		}
	}
	message, err := smtp.ReadDotBytes()
	if err != nil {
		return err
	}
	if len(message) == 0 {
		return errors.New("SMTP DATA was empty")
	}
	close(inFlight)
	select {
	case <-acceptData:
		return smtp.PrintfLine("250 Queued")
	case <-ctx.Done():
		return ctx.Err()
	}
}
