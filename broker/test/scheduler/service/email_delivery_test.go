package sched_service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
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

func (r batchDeliveryCommitAcknowledgement) RecordBatchEmailAcceptance(ctx common.ExtendedContext, runID string) error {
	if err := r.BatchEmailDeliveryRepo.RecordBatchEmailAcceptance(ctx, runID); err != nil {
		return err
	}
	return r.afterCommit()
}

func TestBatchEmailPersistsDeliveryProgress(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		smtpError              error
		loseCommitConfirmation bool
		wantStatus             events.EventStatus
	}{
		{name: "accepted", wantStatus: events.EventStatusSuccess},
		{name: "acceptance commit confirmation lost", loseCommitConfirmation: true, wantStatus: events.EventStatusSuccess},
		{name: "unconfirmed", smtpError: errors.New("SMTP connection lost"), wantStatus: events.EventStatusError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := schedRepo.(*sched_db.PgSchedRepo).Pool
			prRepo := pr_db.CreatePrRepo(pool, false)
			illRepo := ill_db.CreateIllRepo(pool)
			eventRepo := app.CreateEventRepo(pool).(*events.PgEventRepo)

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
			runID := runs[0].ID

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bus := events.NewPostgresEventBus(eventRepo, connString)
			require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
			event, attempt, err := eventRepo.ClaimEventAttemptForSignal(appCtx, runID, events.SignalTaskCreated)
			require.NoError(t, err)
			workCtx := repo.WithBatchAttempt(appCtx, attempt)
			require.ErrorIs(t, schedRepo.RecordBatchEmailIntent(appCtx, runID, []string{pr.ID}, true), repo.ErrBatchAttemptLost)

			sendCount := 0
			var startedAt, acceptedAt pgtype.Timestamptz
			var selectedIDs []string
			var queuePrintedActions bool
			mailer := batchDeliverySMTP(func() error {
				sendCount++
				require.NoError(t, pool.QueryRow(appCtx, `SELECT selected_request_ids, queue_printed_actions, started_at, accepted_at
					FROM batch_action_email_delivery WHERE run_id = $1`, runID).
					Scan(&selectedIDs, &queuePrintedActions, &startedAt, &acceptedAt))
				require.Equal(t, []string{pr.ID}, selectedIDs)
				require.True(t, queuePrintedActions)
				require.True(t, startedAt.Valid)
				require.False(t, acceptedAt.Valid)
				require.Error(t, schedRepo.RecordBatchEmailIntent(workCtx, runID, []string{"different-request"}, false))
				children, err := eventRepo.GetPatronRequestEvents(appCtx, pr.ID)
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
					children, err := eventRepo.GetPatronRequestEvents(appCtx, pr.ID)
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
			emailSender := schedservice.EmailSenderServiceWithClient(prRepo, illRepo, mailer, batchDeliveryPDF{}, bus, deliveryRepo)
			batch := schedservice.NewBatchActionService(bus, prRepo, schedRepo, emailSender)
			batch.BatchAction(workCtx, event)
			require.Equal(t, 1, sendCount)
			if tc.loseCommitConfirmation {
				require.Equal(t, 2, acceptanceWrites)
			}

			stored, err := eventRepo.GetEvent(appCtx, runID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, stored.EventStatus, "%+v", stored.ResultData.EventError)
			require.NoError(t, pool.QueryRow(appCtx, "SELECT selected_request_ids, accepted_at FROM batch_action_email_delivery WHERE run_id = $1", runID).
				Scan(&selectedIDs, &acceptedAt))
			require.Equal(t, []string{pr.ID}, selectedIDs)
			children, err := eventRepo.GetPatronRequestEvents(appCtx, pr.ID)
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

			require.NoError(t, schedRepo.DeleteBatchActionEvents(appCtx, task.ID))
			err = pool.QueryRow(appCtx, "SELECT accepted_at FROM batch_action_email_delivery WHERE run_id = $1", runID).Scan(&acceptedAt)
			require.ErrorIs(t, err, pgx.ErrNoRows)
		})
	}
}
