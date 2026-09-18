package prservice

import (
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecallAction(t *testing.T) {
	old := pgtype.Timestamptz{Time: time.Now().UTC().Add(48 * time.Hour), Valid: true}
	replacement := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                           string
		date                           any
		failedSend, invalid, openEnded bool
	}{
		{name: "omitted"}, {name: "null"}, {name: "open ended", openEnded: true}, {name: "dated", date: replacement.Format(time.RFC3339)},
		{name: "empty", date: "", invalid: true}, {name: "blank", date: " ", invalid: true}, {name: "invalid", date: "bad", invalid: true},
		{name: "wrong type", date: 42, invalid: true}, {name: "send failure", date: replacement.Format(time.RFC3339), failedSend: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := testLoan()
			pr.State = LenderStateRenewalPending
			pr.DueAt = old
			if tc.openEnded {
				pr.DueAt = pgtype.Timestamptz{}
			}
			repo := &MockPrRepo{savedPr: pr}
			sender := &MockIso18626Handler{failSupplyingAgencyMessage: tc.failedSend}
			creator := new(MockLmsCreator)
			creator.On("GetAdapter", pr.SupplierSymbol.String).Return(&lms.LmsAdapterManual{}, nil)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, creator, nil, nil, nil)
			action := LenderActionRecall
			params := map[string]any{"note": "Please return"}
			if tc.date != nil || tc.name == "null" {
				params["dueDate"] = tc.date
			}
			status, _ := svc.handleInvokeAction(appCtx, events.Event{ID: "recall", PatronRequestID: pr.ID, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: params}})
			if tc.invalid || tc.failedSend {
				require.NotEqual(t, events.EventStatusSuccess, status)
				assert.Equal(t, pr.State, repo.savedPr.State)
				assert.Equal(t, pr.DueAt, repo.savedPr.DueAt)
				assert.Equal(t, pr.IllResponse, repo.savedPr.IllResponse)
				return
			}
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, LenderStateRecalled, repo.savedPr.State)
			want := pr.DueAt
			if tc.date != nil {
				want = pgtype.Timestamptz{Time: replacement, Valid: true}
			}
			assert.Equal(t, want, repo.savedPr.DueAt)
			require.NotNil(t, sender.lastSupplyingAgencyMessage)
			assert.Equal(t, iso18626.TypeStatusRecalled, sender.lastSupplyingAgencyMessage.StatusInfo.Status)
			assert.Equal(t, isoLoanDate(want), sender.lastSupplyingAgencyMessage.StatusInfo.DueDate)
			assert.Equal(t, "Please return", sender.lastSupplyingAgencyMessage.MessageInfo.Note)
			assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, sender.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
		})
	}
}

func TestIncomingRecallAndLateMessages(t *testing.T) {
	old := pgtype.Timestamptz{Time: time.Now().UTC().Add(time.Hour), Valid: true}
	date := &utils.XSDDateTime{Time: time.Now().UTC().Add(24 * time.Hour)}
	for _, state := range []pr_db.PatronRequestState{BorrowerStateReceived, BorrowerStateRenewed, BorrowerStateOverdue, BorrowerStateRenewalPending, BorrowerStateRecalled, BorrowerStateShippedReturned} {
		for _, due := range []*utils.XSDDateTime{nil, date} {
			t.Run(string(state)+"/"+string(iso18626.TypeStatusRecalled), func(t *testing.T) {
				pr := testLoan()
				pr.Side = SideBorrowing
				pr.State = state
				pr.DueAt = old
				pr.IllResponse.StatusInfo.Status = iso18626.TypeStatusRecalled
				pr.IllResponse.StatusInfo.DueDate = isoLoanDate(old)
				repo := &MockPrRepo{savedPr: pr}
				handler := CreatePatronRequestMessageHandler(repo, nil, nil, nil)
				sam := iso18626.SupplyingAgencyMessage{MessageInfo: iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusRecalled, DueDate: due}}
				status, _, err := handler.handleSupplyingAgencyMessage(appCtx, sam, pr)
				require.NoError(t, err)
				require.Equal(t, events.EventStatusSuccess, status)
				if state == BorrowerStateRecalled || state == BorrowerStateShippedReturned {
					assert.Equal(t, state, repo.savedPr.State)
					assert.Equal(t, old, repo.savedPr.DueAt)
					assert.Equal(t, pr.IllResponse, repo.savedPr.IllResponse)
				} else {
					assert.Equal(t, BorrowerStateRecalled, repo.savedPr.State)
					assert.True(t, repo.savedPr.NeedsAttention)
					want := old
					if due != nil {
						want = pgtype.Timestamptz{Time: due.Time, Valid: true}
					}
					assert.Equal(t, want, repo.savedPr.DueAt)
				}
			})
		}
	}
	for _, sam := range []iso18626.SupplyingAgencyMessage{
		{MessageInfo: iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusOverdue, DueDate: date}},
		{MessageInfo: iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageRenewResponse, AnswerYesNo: loanYesNo(iso18626.TypeYesNoY)}, StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusLoaned, DueDate: date}},
		{MessageInfo: iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageRenewResponse, AnswerYesNo: loanYesNo(iso18626.TypeYesNoN)}, StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusOverdue}},
	} {
		pr := testLoan()
		pr.Side = SideBorrowing
		pr.State = BorrowerStateRecalled
		pr.DueAt = old
		pr.IllResponse.StatusInfo.Status = iso18626.TypeStatusRecalled
		repo := &MockPrRepo{savedPr: pr}
		handler := CreatePatronRequestMessageHandler(repo, nil, nil, nil)
		status, _, err := handler.handleSupplyingAgencyMessage(appCtx, sam, pr)
		require.NoError(t, err)
		require.Equal(t, events.EventStatusSuccess, status)
		assert.Equal(t, pr.State, repo.savedPr.State)
		assert.Equal(t, old, repo.savedPr.DueAt)
		assert.Equal(t, pr.IllResponse, repo.savedPr.IllResponse)
	}
}

func TestRecallCapabilities(t *testing.T) {
	mapping := mustActionMapping(t)
	for _, state := range []pr_db.PatronRequestState{LenderStateReceived, LenderStateRenewed, LenderStateOverdue, LenderStateRenewalPending, LenderStateShipped, LenderStateRecalled, LenderStateShippedReturn} {
		pr := testLoan()
		pr.State = state
		assert.Equal(t, state == LenderStateReceived || state == LenderStateRenewed || state == LenderStateOverdue || state == LenderStateRenewalPending, mapping.IsActionSupported(pr, LenderActionRecall))
		pr.IllRequest.ServiceInfo.ServiceType = iso18626.TypeServiceTypeCopy
		copyMapping, err := actionMappingService.GetActionMapping(pr.IllRequest)
		require.NoError(t, err)
		assert.False(t, copyMapping.IsActionSupported(pr, LenderActionRecall))
	}
	pr := testLoan()
	pr.Side = SideBorrowing
	pr.State = BorrowerStateRecalled
	assert.True(t, mapping.IsActionSupported(pr, BorrowerActionShipReturn))
	assert.True(t, mapping.IsActionSupported(pr, BorrowerActionCheckIn))
	assert.False(t, mapping.IsActionSupported(pr, BorrowerActionCheckOut))
	assert.False(t, mapping.IsActionSupported(pr, BorrowerActionRenew))
}

func TestRecalledLoanReturn(t *testing.T) {
	pr := testLoan()
	pr.State = LenderStateRecalled
	repo := &MockPrRepo{savedPr: pr}
	handler := CreatePatronRequestMessageHandler(repo, nil, nil, nil)
	status, response, err := handler.handleRequestingAgencyMessage(appCtx, iso18626.RequestingAgencyMessage{
		Action: iso18626.TypeActionShippedReturn,
	}, pr)
	require.NoError(t, err)
	require.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, response.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, LenderStateShippedReturn, repo.savedPr.State)
	assert.True(t, mustActionMapping(t).IsActionSupported(repo.savedPr, LenderActionMarkReceived))
}
