package prservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRunAutoActionsStatusAndBinding(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     events.EventStatus
		outcome    string
		target     string
		childError bool
		processErr bool
		readErr    bool
		wantErr    string
		wantTasks  int
	}{
		{name: "unbound success continues", status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, wantTasks: 2},
		{name: "success failure outcome continues", status: events.EventStatusSuccess, outcome: ActionOutcomeFailure, wantTasks: 2},
		{name: "bound problem failure continues", status: events.EventStatusProblem, outcome: ActionOutcomeFailure, target: string(LenderStateNew), wantTasks: 2},
		{name: "bound problem review continues", status: events.EventStatusProblem, outcome: ActionOutcomeReview, target: string(LenderStateNew), wantTasks: 2},
		{name: "bound problem success outcome continues", status: events.EventStatusProblem, outcome: ActionOutcomeSuccess, target: string(LenderStateNew), wantTasks: 2},
		{name: "problem changes state", status: events.EventStatusProblem, outcome: ActionOutcomeReview, target: string(LenderStateValidated), wantTasks: 1},
		{name: "unbound problem propagates", status: events.EventStatusProblem, outcome: ActionOutcomeFailure, wantErr: "failed with status PROBLEM", wantTasks: 1},
		{name: "bound error failure stops", status: events.EventStatusError, outcome: ActionOutcomeFailure, target: string(LenderStateNew), wantTasks: 1},
		{name: "bound error review stops", status: events.EventStatusError, outcome: ActionOutcomeReview, target: string(LenderStateNew), wantTasks: 1},
		{name: "bound error success outcome stops", status: events.EventStatusError, outcome: ActionOutcomeSuccess, target: string(LenderStateNew), wantTasks: 1},
		{name: "unbound error propagates", status: events.EventStatusError, outcome: ActionOutcomeFailure, wantErr: "failed with status ERROR", wantTasks: 1},
		{name: "bound problem child error propagates", status: events.EventStatusProblem, outcome: ActionOutcomeFailure, target: string(LenderStateNew), childError: true, wantErr: "child failed", wantTasks: 1},
		{name: "bound error child error propagates", status: events.EventStatusError, outcome: ActionOutcomeReview, target: string(LenderStateNew), childError: true, wantErr: "child failed", wantTasks: 1},
		{name: "task processing error propagates", status: events.EventStatusProblem, outcome: ActionOutcomeFailure, target: string(LenderStateNew), processErr: true, wantErr: "task completion failed", wantTasks: 1},
		{name: "request reload error propagates", status: events.EventStatusProblem, outcome: ActionOutcomeFailure, target: string(LenderStateNew), readErr: true, wantErr: "reload failed", wantTasks: 1},
		{name: "incomplete status propagates even with binding", status: events.EventStatusProcessing, outcome: ActionOutcomeFailure, target: string(LenderStateNew), wantErr: "failed with status PROCESSING", wantTasks: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := pr_db.PatronRequest{ID: patronRequestId, State: LenderStateNew, Side: SideLending}
			prRepo := new(MockPrRepo)
			bus := new(MockEventBus)
			result := &events.ActionResult{Outcome: tc.outcome}
			if tc.target != "" {
				result.ToState = &tc.target
			}
			if tc.childError {
				result.ChildActionError = ptr("child failed")
			}
			var processErr error
			if tc.processErr {
				processErr = errors.New("task completion failed")
			}
			bus.On("ProcessExclusiveTask", patronRequestId+"-task-1").Return(events.Event{
				EventStatus: tc.status,
				ResultData:  events.EventResult{CommonEventData: events.CommonEventData{ActionResult: result}},
			}, processErr).Once()
			if tc.wantTasks == 2 || tc.target == string(LenderStateValidated) || tc.readErr {
				updatedPr := pr
				if tc.target != "" {
					updatedPr.State = pr_db.PatronRequestState(tc.target)
				}
				var readErr error
				if tc.readErr {
					readErr = errors.New("reload failed")
				}
				prRepo.On("GetPatronRequestById", pr.ID).Return(updatedPr, readErr)
			}
			if tc.wantTasks == 2 {
				bus.On("ProcessExclusiveTask", patronRequestId+"-task-2").Return(events.Event{EventStatus: events.EventStatusSuccess}, nil).Once()
			}
			svc := CreatePatronRequestActionService(prRepo, new(IllRepoMock), bus, new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)

			err := svc.RunAutoActionsOnStateEntry(appCtx, pr, nil, "test-user")

			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
			assert.Len(t, bus.createdTaskData, tc.wantTasks)
			bus.AssertExpectations(t)
			prRepo.AssertExpectations(t)
		})
	}
}

func TestFinalizeActionAttentionUsesStatusAndResultingState(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         events.EventStatus
		outcome        string
		bound          bool
		stateAttention bool
		wantAttention  bool
	}{
		{name: "unbound problem review forces attention", status: events.EventStatusProblem, outcome: ActionOutcomeReview, wantAttention: true},
		{name: "bound error review forces attention", status: events.EventStatusError, outcome: ActionOutcomeReview, bound: true, wantAttention: true},
		{name: "unbound error success outcome forces attention", status: events.EventStatusError, outcome: ActionOutcomeSuccess, wantAttention: true},
		{name: "problem self transition forces attention", status: events.EventStatusProblem, outcome: ActionOutcomeFailure, bound: true, wantAttention: true},
		{name: "successful failure outcome does not force attention", status: events.EventStatusSuccess, outcome: ActionOutcomeFailure},
		{name: "success clears prior attention without transition", status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess},
		{name: "success preserves state attention without transition", status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, stateAttention: true, wantAttention: true},
		{name: "success preserves state attention with self transition", status: events.EventStatusSuccess, outcome: ActionOutcomeReview, bound: true, stateAttention: true, wantAttention: true},
		{name: "success self transition clears prior attention", status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, bound: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateNew, Side: SideBorrowing, NeedsAttention: true}
			stateModel, err := LoadStateModelByName("default")
			require.NoError(t, err)
			mapping := NewActionMappingForServiceType(stateModel, proapi.Loan)
			config := mapping.borrowerStateConfig[pr.State]
			config.needsAttention = tc.stateAttention
			action := proapi.ModelAction{Name: string(BorrowerActionValidatePatron)}
			if tc.bound {
				require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(`{"name":"validate-patron","transitions":{"%s":"NEW"}}`, tc.outcome)), &action))
			}
			config.actions[BorrowerActionValidatePatron] = action
			mapping.borrowerStateConfig[pr.State] = config
			repo := new(MockPrRepo)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
			status, result := svc.finalizeActionExecution(appCtx, events.Event{}, mapping, BorrowerActionValidatePatron, pr, actionExecutionResult{
				status: tc.status, pr: pr,
				result: &events.EventResult{CommonEventData: events.CommonEventData{ActionResult: &events.ActionResult{Outcome: tc.outcome}}},
			})
			assert.Equal(t, tc.status, status)
			assert.Equal(t, tc.outcome, result.ActionResult.Outcome)
			assert.Equal(t, tc.wantAttention, repo.savedPr.NeedsAttention)
			assert.Equal(t, pr.State, repo.savedPr.State)
		})
	}
}

func TestBoundNotificationProblemContinuesValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		smtp bool
	}{
		{name: "template expansion", body: "{{.PatronGivenName}}"},
		{name: "SMTP", body: "New request", smtp: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// RequestItem stops the downstream loan workflow in ITEM_PENDING, so
			// this exercises notification, validation, and recovery end to end.
			pr := pr_db.PatronRequest{ID: patronRequestId, State: LenderStateNew, Side: SideLending,
				SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}
			repo := &actionHistoryRepo{MockPrRepo: &MockPrRepo{savedPr: pr}}
			bus := new(MockEventBus)
			lmsCreator := new(MockLmsCreator)
			lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(&MockLmsAdapterLog{requestItemErr: errors.New("reservation failed")}, nil)
			emailSvc := new(EmailSenderMock)
			emailSvc.On("IsReadyToSend").Return(true)
			if tc.smtp {
				emailSvc.On("SendEmail", testFrom).Return(errors.New("SMTP failed")).Once()
			}
			illRepo := new(IllRepoMock)
			illRepo.On("GetPeerBySymbol", "ISIL:SUP1").Return(peerWithFromEmailOnly(testFrom), nil)
			repo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(pr_db.Template{ID: "template-1", Body: tc.body}, nil)
			repo.On("GetItemsByPrId", pr.ID).Return([]pr_db.Item{}, nil)
			svc := CreatePatronRequestActionService(repo, illRepo, bus, new(MockIso18626Handler), lmsCreator, emailSvc, nil, nil)

			require.NoError(t, svc.RunAutoActionsOnStateEntry(appCtx, pr, nil, "test-user"))

			require.Len(t, bus.processedTaskEvents, 3)
			var notification, validation events.Event
			for _, event := range bus.processedTaskEvents {
				switch *event.EventData.Action {
				case LenderActionSendNotification:
					notification = event
				case LenderActionValidatePatron:
					validation = event
				}
			}
			assert.Equal(t, events.EventStatusProblem, notification.EventStatus)
			assert.Equal(t, ActionOutcomeFailure, notification.ResultData.ActionResult.Outcome)
			assert.Equal(t, string(LenderStateNew), *notification.ResultData.ActionResult.ToState)
			assert.Contains(t, notification.ResultData.Problem.Details, "error sending email to staff")
			if tc.smtp {
				assert.Contains(t, notification.ResultData.Problem.Details, "SMTP failed")
			} else {
				assert.Contains(t, notification.ResultData.Problem.Details, "PatronGivenName")
				assert.Contains(t, notification.ResultData.Problem.Details, "template-1")
			}
			assert.Equal(t, events.EventStatusSuccess, validation.EventStatus)
			assert.Nil(t, validation.ResultData.ActionResult.ChildActionError)
			require.Len(t, repo.savedRequests, 3)
			assert.True(t, repo.savedRequests[0].NeedsAttention)
			assert.Equal(t, LenderStateNew, repo.savedRequests[0].State)
			assert.False(t, repo.savedRequests[1].NeedsAttention)
			assert.Equal(t, LenderStateValidated, repo.savedRequests[1].State)
			assert.Equal(t, LenderStateItemPending, repo.savedPr.State)
			emailSvc.AssertExpectations(t)
		})
	}
}

func TestBoundPatronReviewDoesNotPropagate(t *testing.T) {
	pr := pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateNew, Side: SideBorrowing, RequesterSymbol: getDbText("ISIL:REC1")}
	repo := &MockPrRepo{savedPr: pr}
	bus := new(MockEventBus)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(&MockLmsAdapterPatronProblem{}, nil)
	svc := CreatePatronRequestActionService(repo, new(IllRepoMock), bus, new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)

	require.NoError(t, svc.RunAutoActionsOnStateEntry(appCtx, pr, nil, "test-user"))

	require.Len(t, bus.processedTaskEvents, 1)
	assert.Equal(t, events.EventStatusProblem, bus.processedTaskEvents[0].EventStatus)
	assert.Equal(t, ActionOutcomeReview, bus.processedTaskEvents[0].ResultData.ActionResult.Outcome)
	assert.Equal(t, BorrowerStateInvalidPatron, repo.savedPr.State)
	assert.True(t, repo.savedPr.NeedsAttention)
}

func TestManualNotificationRetryUsesStateModelParams(t *testing.T) {
	for _, tc := range []struct {
		name     string
		side     pr_db.PatronRequestSide
		state    pr_db.PatronRequestState
		audience proapi.ModelActionParamsSendTo
		label    string
		extra    bool
		override bool
	}{
		{name: "patron", side: SideBorrowing, state: BorrowerStateReceived, audience: proapi.ModelActionParamsSendToPatron, label: "received-notification"},
		{name: "staff", side: SideLending, state: LenderStateNew, audience: proapi.ModelActionParamsSendToStaff, label: "new-supply-request-notification"},
		{name: "other custom data", side: SideBorrowing, state: BorrowerStateReceived, audience: proapi.ModelActionParamsSendToPatron, label: "received-notification", extra: true},
		{name: "explicit parameters", side: SideBorrowing, state: BorrowerStateReceived, audience: proapi.ModelActionParamsSendToPatron, label: "retry-template", override: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := prWithPatronEmail(testPatronTo)
			pr.ID, pr.Side, pr.State = patronRequestId, tc.side, tc.state
			pr.RequesterSymbol, pr.SupplierSymbol = getDbText(testSymbol), getDbText(testSymbol)
			repo := &MockPrRepo{savedPr: pr}
			bus := new(MockEventBus)
			lmsCreator := new(MockLmsCreator)
			lmsCreator.On("GetAdapter", testSymbol).Return(lms.CreateLmsAdapterMockOK(), nil)
			sender := new(notificationRetryEmailRecorder)
			illRepo := new(IllRepoMock)
			illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithFromEmailOnly(testFrom), nil).Once()
			repo.On("GetTemplateByPurposeAudienceLabelAndOwner", pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams{
				Purpose: string(proapi.Email), Owner: testSymbol, Label: tc.label, Audience: string(tc.audience),
			}).Return(pr_db.Template{Body: "Your notification", Subject: getDbText("Notification")}, nil).Once()
			svc := CreatePatronRequestActionService(repo, illRepo, bus, new(MockIso18626Handler), lmsCreator, sender, nil, nil)
			mapping, err := svc.actionMappingService.GetActionMapping(pr.IllRequest)
			require.NoError(t, err)
			action := BorrowerActionSendNotification // The action name is shared by both sides.
			event := events.Event{ID: "notification", PatronRequestID: pr.ID, EventData: events.EventData{
				CommonEventData: events.CommonEventData{Action: &action},
				CustomData:      map[string]any{"staticActionParams": mapping.GetAutoActionsForState(pr)[0].Params},
			}}

			status, result := svc.handleInvokeAction(appCtx, event)

			require.Equal(t, events.EventStatusProblem, status)
			require.Equal(t, ActionOutcomeFailure, result.ActionResult.Outcome)
			require.True(t, repo.savedPr.NeedsAttention)
			require.Zero(t, sender.sends)
			var retryAvailable bool
			for _, allowed := range mapping.GetAllowedActionsForPatronRequest(repo.savedPr, true).Actions {
				if allowed.Name == string(action) {
					retryAvailable = true
					assert.Empty(t, allowed.Parameters)
				}
			}
			require.True(t, retryAvailable, "the failed notification must be available for manual retry")

			// Manual API calls carry no staticActionParams. Other custom data must
			// survive resolution, and explicitly supplied parameters still win.
			event.ID = "notification-retry"
			event.EventData.CustomData = nil
			if tc.extra {
				event.EventData.CustomData = map[string]any{"note": "retry notification"}
			}
			if tc.override {
				event.EventData.CustomData = map[string]any{"staticActionParams": staticParams(tc.label, tc.audience).StaticActionParams}
			}
			sender.ready = true

			status, result = svc.handleInvokeAction(appCtx, event)

			assert.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, ActionOutcomeSuccess, result.ActionResult.Outcome)
			assert.False(t, repo.savedPr.NeedsAttention)
			assert.Equal(t, tc.state, repo.savedPr.State)
			require.Equal(t, 1, sender.sends)
			assert.Equal(t, testFrom, sender.from)
			recipient := testPatronTo
			if tc.audience == proapi.ModelActionParamsSendToStaff {
				recipient = testFrom
			}
			assert.Equal(t, []string{recipient}, sender.recipients)
			assert.Contains(t, string(sender.raw), "Your notification")
			assert.Empty(t, bus.createdTaskData, "a retry must not restart state entry actions")
			if tc.extra {
				assert.Equal(t, map[string]any{"note": "retry notification"}, event.EventData.CustomData)
			}
			repo.AssertExpectations(t)
			illRepo.AssertExpectations(t)
		})
	}
}

func TestStaticActionParamsPreserveTaskDataAndPrecedence(t *testing.T) {
	declared := &proapi.ModelAction_Params{AdditionalProperties: map[string]any{"location": "configured"}}
	explicit := &proapi.ModelAction_Params{AdditionalProperties: map[string]any{"location": "explicit"}}
	for _, tc := range []struct {
		name       string
		customData map[string]any
		params     *proapi.ModelAction_Params
		want       map[string]any
	}{
		{name: "parameterless task", params: declared, want: map[string]any{"staticActionParams": declared}},
		{name: "other task data", customData: map[string]any{"note": "staff input"}, params: declared,
			want: map[string]any{"note": "staff input", "staticActionParams": declared}},
		{name: "explicit task parameters", customData: map[string]any{"note": "staff input", "staticActionParams": explicit}, params: declared,
			want: map[string]any{"note": "staff input", "staticActionParams": explicit}},
		{name: "null task parameters", customData: map[string]any{"staticActionParams": nil}, params: declared,
			want: map[string]any{"staticActionParams": declared}},
		{name: "no declaration parameters", customData: map[string]any{"note": "staff input"}, want: map[string]any{"note": "staff input"}},
		{name: "no parameters or task data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := maps.Clone(tc.customData)

			resolved := withStaticActionParams(tc.customData, tc.params)

			assert.Equal(t, tc.want, resolved)
			assert.Equal(t, original, tc.customData, "resolving declaration parameters must not alter the original task data")
			assert.Equal(t, resolved, withStaticActionParams(resolved, tc.params), "parameters captured on automatic tasks must survive execution")
		})
	}
}

type notificationRetryEmailRecorder struct {
	ready      bool
	sends      int
	from       string
	recipients []string
	raw        []byte
}

func (s *notificationRetryEmailRecorder) IsReadyToSend() bool { return s.ready }

func (s *notificationRetryEmailRecorder) SendEmail(from string, recipients []string, raw []byte) error {
	s.sends++
	s.from, s.recipients, s.raw = from, recipients, raw
	return nil
}

func TestTransitionIsNotHandledWhenPersistenceFails(t *testing.T) {
	for _, step := range []string{"update", "commit"} {
		t.Run(step, func(t *testing.T) {
			pr := pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateNew, Side: SideBorrowing, RequesterSymbol: getDbText("ISIL:REC1")}
			if step == "update" {
				pr.ID = "pr-error"
			}
			baseRepo := &MockPrRepo{savedPr: pr}
			var repo pr_db.PrRepo = baseRepo
			if step == "commit" {
				repo = &loanTxRepo{MockPrRepo: baseRepo, failStep: "commit"}
			}
			bus := new(MockEventBus)
			lmsCreator := new(MockLmsCreator)
			lmsCreator.On("GetAdapter", "ISIL:REC1").Return(&MockLmsAdapterPatronProblem{}, nil)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), bus, new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)

			assert.ErrorContains(t, svc.RunAutoActionsOnStateEntry(appCtx, pr, nil, "test-user"), "failed to persist patron request")

			require.Len(t, bus.processedTaskEvents, 1)
			assert.Equal(t, events.EventStatusError, bus.processedTaskEvents[0].EventStatus)
			assert.Nil(t, bus.processedTaskEvents[0].ResultData.ActionResult.ToState)
			assert.Equal(t, BorrowerStateNew, baseRepo.savedPr.State)
		})
	}
}

type actionHistoryRepo struct {
	*MockPrRepo
	savedRequests []pr_db.PatronRequest
}

func (r *actionHistoryRepo) UpdatePatronRequest(ctx common.ExtendedContext, params pr_db.UpdatePatronRequestParams) (pr_db.PatronRequest, error) {
	pr, err := r.MockPrRepo.UpdatePatronRequest(ctx, params)
	if err == nil {
		r.savedRequests = append(r.savedRequests, pr)
	}
	return pr, err
}

func (r *actionHistoryRepo) WithTxFunc(ctx common.ExtendedContext, fn func(pr_db.PrRepo) error) error {
	return fn(r)
}
