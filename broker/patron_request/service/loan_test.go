package prservice

import (
	"errors"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestLoanCalendarDates(t *testing.T) {
	zone := "Europe/Copenhagen"
	entry := dirapi.Entry{TimeZone: &zone, IllConfig: &dirapi.IllConfig{}}
	entry.IllConfig.DefaultLoanPeriod.Set(2)
	now := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	due, err := defaultLoanDate(entry, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-03-30T23:59:59+02:00", due.Format(time.RFC3339))
	manual, err := parseLoanDate("2026-10-25", entry)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-25T23:59:59+01:00", manual.Format(time.RFC3339))
	for _, invalid := range []string{"invalid", "2026-02-30", "2026-01-01T12:00:00", "0001-01-01T00:00:00Z"} {
		_, err := parseLoanDate(invalid, entry)
		require.Error(t, err, invalid)
	}
	zone = "invalid/zone"
	_, err = parseLoanDate("2026-01-01", entry)
	require.Error(t, err)
	_, err = parseLoanDate("2026-01-01T12:00:00Z", entry)
	require.NoError(t, err, "timezone is irrelevant to an explicit timestamp")
	date, err := defaultLoanDate(dirapi.Entry{}, now)
	require.NoError(t, err)
	assert.Nil(t, date)
	date, err = parseLoanDate("2026-01-01", dirapi.Entry{})
	require.NoError(t, err)
	assert.Equal(t, "2026-01-01T23:59:59Z", date.Format(time.RFC3339))
}

func TestResolveLoanDueDate(t *testing.T) {
	now := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	manual := now.AddDate(0, 0, 20)
	earliest := now.AddDate(0, 0, 5)
	later := now.AddDate(0, 0, 10)
	zone := "Europe/Copenhagen"
	entry := dirapi.Entry{TimeZone: &zone, IllConfig: &dirapi.IllConfig{}}
	entry.IllConfig.DefaultLoanPeriod.Set(2)
	invalidZone := "invalid/zone"
	invalidDefault := dirapi.Entry{IllConfig: &dirapi.IllConfig{}}
	invalidDefault.IllConfig.DefaultLoanPeriod.Set(0)
	for _, tc := range []struct {
		name   string
		items  []pr_db.Item
		manual *time.Time
		entry  dirapi.Entry
		want   time.Time
		source string
		err    string
	}{
		{name: "earliest checkout wins", items: []pr_db.Item{
			{LmsDueDate: pgtype.Timestamptz{Time: later, Valid: true}},
			{LmsDueDate: pgtype.Timestamptz{Time: earliest, Valid: true}},
		}, manual: &manual, entry: entry, want: earliest, source: "LMS checkout"},
		{name: "invalid checkout dates ignored", items: []pr_db.Item{
			{LmsDueDate: pgtype.Timestamptz{Time: earliest}},
			{LmsDueDate: pgtype.Timestamptz{Valid: true}},
		}, manual: &manual, entry: entry, want: manual, source: "ship.dueDate"},
		{name: "calendar default", entry: entry, want: time.Date(2026, 3, 30, 21, 59, 59, 0, time.UTC), source: "illConfig.defaultLoanPeriod"},
		{name: "no date"},
		{name: "unset default", entry: dirapi.Entry{IllConfig: &dirapi.IllConfig{}}},
		{name: "invalid default period", entry: invalidDefault, err: "defaultLoanPeriod must be a positive integer"},
		{name: "invalid default timezone", entry: dirapi.Entry{TimeZone: &invalidZone, IllConfig: entry.IllConfig}, err: "unknown time zone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			due, source, err := resolveLoanDueDate(tc.items, tc.manual, tc.entry, now)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			if tc.want.IsZero() {
				assert.Nil(t, due)
			} else {
				require.NotNil(t, due)
				assert.True(t, tc.want.Equal(*due), "want %v, got %v", tc.want, due)
			}
			assert.Equal(t, tc.source, source)
		})
	}
}

func testLoan() pr_db.PatronRequest {
	return pr_db.PatronRequest{ID: "loan", State: LenderStateWillSupply, Side: SideLending,
		SupplierSymbol: getDbText("ISIL:SUP"), RequesterSymbol: getDbText("ISIL:REQ"),
		IllRequest:  iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}},
		IllResponse: iso18626.SupplyingAgencyMessage{StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusLoaned}}}
}

func TestShipCheckpointAndDueDatePrecedence(t *testing.T) {
	repo := &MockPrRepo{savedPr: testLoan(), savedItems: []pr_db.Item{
		{ID: "a", PrID: "loan", Barcode: "a", LmsStatus: pr_db.LmsStatusUnknown},
		{ID: "b", PrID: "loan", Barcode: "b", LmsStatus: pr_db.LmsStatusRequested},
	}}
	adapter := new(mockLmsAdapter)
	first := time.Now().UTC().AddDate(0, 0, 10)
	earliest := first.AddDate(0, 0, -2)
	adapter.On("CheckOutItem", "", "a", "", "").Return(&lms.CheckedOutItem{DueDate: &first}, nil).Once()
	failed := adapter.On("CheckOutItem", "", "b", "", "").Return(nil, errors.New("LMS unavailable")).Once()
	sender := new(MockIso18626Handler)
	svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, nil, nil, nil, nil)
	result := svc.shipLenderRequest(appCtx, "event", repo.savedPr, adapter, repo.savedPr.IllRequest, actionParams{DueDate: "2030-01-01"})
	assert.Equal(t, events.EventStatusError, result.status)
	assert.False(t, result.pr.DueAt.Valid, "fallback must not finalize before all checkouts")
	assert.Equal(t, pr_db.LmsStatusCheckedOut, repo.savedItems[0].LmsStatus)
	assert.Equal(t, pr_db.LmsStatusRequested, repo.savedItems[1].LmsStatus)
	assert.Nil(t, sender.lastSupplyingAgencyMessage)
	failed.Unset()
	adapter.On("CheckOutItem", "", "b", "", "").Return(&lms.CheckedOutItem{DueDate: &earliest}, nil).Once()
	result = svc.shipLenderRequest(appCtx, "retry", result.pr, adapter, result.pr.IllRequest, actionParams{DueDate: "2030-01-01"})
	require.Equal(t, events.EventStatusSuccess, result.status)
	assert.Equal(t, earliest, result.pr.DueAt.Time)
	assert.Equal(t, earliest, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate.Time)
	// A delivery retry uses the saved date and does not repeat either checkout.
	result = svc.shipLenderRequest(appCtx, "retry-delivery", result.pr, adapter, result.pr.IllRequest, actionParams{DueDate: "2031-01-01"})
	require.Equal(t, events.EventStatusSuccess, result.status)
	assert.Equal(t, earliest, result.pr.DueAt.Time)
	adapter.AssertExpectations(t)
	adapter.AssertNumberOfCalls(t, "CheckOutItem", 3)
}

func TestShippingWithoutDateRetainsCompletedCheckout(t *testing.T) {
	repo := &MockPrRepo{savedPr: testLoan(), savedItems: []pr_db.Item{{ID: "a", PrID: "loan", Barcode: "a"}}}
	directory := new(IllRepoMock)
	directory.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything).Return([]ill_db.Peer{{CustomData: dirapi.Entry{}}}, "", nil)
	adapter := new(mockLmsAdapter)
	adapter.On("CheckOutItem", "", "a", "", "").Return(&lms.CheckedOutItem{}, nil).Once()
	sender := new(MockIso18626Handler)
	svc := CreatePatronRequestActionService(repo, directory, new(MockEventBus), sender, nil, nil, nil, nil)
	result := svc.shipLenderRequest(appCtx, "event", repo.savedPr, adapter, repo.savedPr.IllRequest, actionParams{})
	require.Equal(t, events.EventStatusSuccess, result.status)
	require.Equal(t, pr_db.LmsStatusCheckedOut, repo.savedItems[0].LmsStatus)
	assert.False(t, result.pr.DueAt.Valid)
	require.NotNil(t, sender.lastSupplyingAgencyMessage)
	assert.Equal(t, iso18626.TypeStatusLoaned, sender.lastSupplyingAgencyMessage.StatusInfo.Status)
	assert.Nil(t, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate)
	result = svc.shipLenderRequest(appCtx, "retry", result.pr, adapter, result.pr.IllRequest, actionParams{})
	require.Equal(t, events.EventStatusSuccess, result.status)
	assert.False(t, result.pr.DueAt.Valid)
	assert.Nil(t, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate)
	adapter.AssertNumberOfCalls(t, "CheckOutItem", 1)
}

func TestShippingDateSourceStaysOnActionResult(t *testing.T) {
	for _, manual := range []bool{false, true} {
		for _, failedSend := range []bool{false, true} {
			pr := testLoan()
			repo := &MockPrRepo{savedPr: pr, savedItems: []pr_db.Item{{ID: "item", PrID: pr.ID, Barcode: "item"}}}
			entry := dirapi.Entry{IllConfig: &dirapi.IllConfig{}}
			entry.IllConfig.DefaultLoanPeriod.Set(14)
			directory := new(IllRepoMock)
			directory.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything).Return([]ill_db.Peer{{CustomData: entry}}, "", nil)
			bus := new(MockEventBus)
			sender := &MockIso18626Handler{failSupplyingAgencyMessage: failedSend}
			svc := CreatePatronRequestActionService(repo, directory, bus, sender, nil, nil, nil, nil)
			params := actionParams{}
			source := "illConfig.defaultLoanPeriod"
			if manual {
				params.DueDate, source = "2030-01-01", "ship.dueDate"
			}
			result := svc.shipLenderRequest(appCtx, "event", pr, &lms.LmsAdapterManual{}, pr.IllRequest, params)
			if failedSend {
				require.NotEqual(t, events.EventStatusSuccess, result.status)
			} else {
				require.Equal(t, events.EventStatusSuccess, result.status)
			}
			resolution, ok := result.result.CustomData["dueDateResolution"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, source, resolution["source"])
			assert.Equal(t, result.pr.DueAt.Time, resolution["dueDate"])
			assert.EqualValues(t, 14, resolution["defaultLoanPeriod"])
			assert.Equal(t, []events.EventName{events.EventNameIllSupplierMessage}, bus.createdNoticeNames)
			// A later delivery retry retains the frozen date, without claiming it
			// was recalculated from a newly supplied manual date.
			sender.failSupplyingAgencyMessage = false
			retry := svc.shipLenderRequest(appCtx, "retry", result.pr, &lms.LmsAdapterManual{}, pr.IllRequest, actionParams{DueDate: "2031-01-01"})
			require.Equal(t, events.EventStatusSuccess, retry.status)
			assert.Equal(t, result.pr.DueAt, retry.pr.DueAt)
			assert.NotContains(t, retry.result.CustomData, "dueDateResolution")
		}
	}
}

func TestIncomingLoanAndRenewalDates(t *testing.T) {
	old := time.Now().UTC().AddDate(0, 0, -30)
	past := old.AddDate(0, 0, 1)
	yearZero := &utils.XSDDateTime{Time: time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)}
	for _, tc := range []struct {
		name   string
		state  pr_db.PatronRequestState
		reason iso18626.TypeReasonForMessage
		answer *iso18626.TypeYesNo
		date   *utils.XSDDateTime
		want   pr_db.PatronRequestState
		fail   bool
	}{
		{name: "open-ended shipment", state: BorrowerStateWillSupply, reason: iso18626.TypeReasonForMessageStatusChange, want: BorrowerStateShipped},
		{name: "accept past date without local overdue decision", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), date: &utils.XSDDateTime{Time: past}, want: BorrowerStateRenewed},
		{name: "undated acceptance clears previous date", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), want: BorrowerStateRenewed},
		{name: "reject zero renewal date", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), date: &utils.XSDDateTime{}, fail: true},
		{name: "reject year-zero renewal", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), date: yearZero, fail: true},
		{name: "reject year-zero shipment", state: BorrowerStateWillSupply, reason: iso18626.TypeReasonForMessageStatusChange, date: yearZero, fail: true},
		{name: "reject preserves date", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoN), want: BorrowerStateOverdue},
		{name: "unsolicited response", state: BorrowerStateReceived, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), date: &utils.XSDDateTime{Time: past}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(MockPrRepo)
			handler := CreatePatronRequestMessageHandler(repo, nil, nil, nil)
			pr := testLoan()
			pr.Side = SideBorrowing
			pr.State = tc.state
			if tc.reason == iso18626.TypeReasonForMessageRenewResponse {
				pr.DueAt = pgtype.Timestamptz{Time: old, Valid: true}
				pr.IllResponse.StatusInfo.DueDate = isoLoanDate(pr.DueAt)
			}
			status, _, _ := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
				MessageInfo: iso18626.MessageInfo{ReasonForMessage: tc.reason, AnswerYesNo: tc.answer},
				StatusInfo:  iso18626.StatusInfo{Status: iso18626.TypeStatusLoaned, DueDate: tc.date},
			}, pr)
			if tc.fail {
				assert.Equal(t, events.EventStatusProblem, status)
				assert.Empty(t, repo.savedPr.ID)
				return
			}
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, tc.want, repo.savedPr.State)
			if tc.reason == iso18626.TypeReasonForMessageRenewResponse {
				assert.Empty(t, repo.savedItems, "renewal must not create shipment items")
				if *tc.answer == iso18626.TypeYesNoY {
					if tc.date == nil {
						assert.Equal(t, pgtype.Timestamptz{}, repo.savedPr.DueAt)
						assert.Nil(t, repo.savedPr.IllResponse.StatusInfo.DueDate)
					} else {
						assert.True(t, repo.savedPr.DueAt.Valid)
						assert.Equal(t, tc.date.Time, repo.savedPr.DueAt.Time)
					}
				} else {
					assert.Equal(t, old, repo.savedPr.DueAt.Time)
					require.NotNil(t, repo.savedPr.IllResponse.StatusInfo.DueDate)
					assert.Equal(t, old, repo.savedPr.IllResponse.StatusInfo.DueDate.Time)
				}
			} else {
				assert.False(t, repo.savedPr.DueAt.Valid)
			}
		})
	}
}

func loanYesNo(value iso18626.TypeYesNo) *iso18626.TypeYesNo { return &value }

func TestIncomingLoanStatusPreservesShipmentDetails(t *testing.T) {
	old := time.Now().UTC().Add(-time.Hour)
	updated := old.Add(48 * time.Hour)
	for _, tc := range []struct {
		name   string
		reason iso18626.TypeReasonForMessage
		answer *iso18626.TypeYesNo
		status iso18626.TypeStatus
	}{
		{"overdue", iso18626.TypeReasonForMessageStatusChange, nil, iso18626.TypeStatusOverdue},
		{"accepted", iso18626.TypeReasonForMessageRenewResponse, loanYesNo(iso18626.TypeYesNoY), iso18626.TypeStatusLoaned},
		{"rejected", iso18626.TypeReasonForMessageRenewResponse, loanYesNo(iso18626.TypeYesNoN), iso18626.TypeStatusOverdue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := testLoan()
			pr.Side, pr.State = SideBorrowing, BorrowerStateRenewalPending
			if tc.answer == nil {
				pr.State = BorrowerStateReceived
			}
			pr.DueAt = pgtype.Timestamptz{Time: old, Valid: true}
			pr.IllResponse.StatusInfo.ExpectedDeliveryDate = &utils.XSDDateTime{Time: old}
			pr.IllResponse.MessageInfo = iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange, AnswerYesNo: loanYesNo(iso18626.TypeYesNoN), Note: "old note"}
			pr.IllResponse.DeliveryInfo = &iso18626.DeliveryInfo{ItemId: "shipment"}
			pr.IllResponse.ReturnInfo = &iso18626.ReturnInfo{PhysicalAddress: &iso18626.PhysicalAddress{}}
			sam := iso18626.SupplyingAgencyMessage{
				MessageInfo: iso18626.MessageInfo{ReasonForMessage: tc.reason, AnswerYesNo: tc.answer, Note: "updated loan note"},
				StatusInfo:  iso18626.StatusInfo{Status: tc.status, LastChange: utils.XSDDateTime{Time: time.Now().UTC()}},
			}
			wantDue := old
			if tc.answer != nil && *tc.answer == iso18626.TypeYesNoY {
				sam.StatusInfo.DueDate = &utils.XSDDateTime{Time: updated}
				wantDue = updated
			}
			repo := new(MockPrRepo)
			handler := CreatePatronRequestMessageHandler(repo, nil, nil, nil)
			status, _, err := handler.handleSupplyingAgencyMessage(appCtx, sam, pr)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, tc.status, repo.savedPr.IllResponse.StatusInfo.Status)
			assert.Equal(t, sam.MessageInfo, repo.savedPr.IllResponse.MessageInfo)
			assert.Equal(t, sam.StatusInfo.LastChange, repo.savedPr.IllResponse.StatusInfo.LastChange)
			assert.Equal(t, pr.IllResponse.StatusInfo.ExpectedDeliveryDate, repo.savedPr.IllResponse.StatusInfo.ExpectedDeliveryDate)
			assert.Equal(t, wantDue, repo.savedPr.DueAt.Time)
			require.NotNil(t, repo.savedPr.IllResponse.StatusInfo.DueDate)
			assert.Equal(t, wantDue, repo.savedPr.IllResponse.StatusInfo.DueDate.Time)
			assert.Equal(t, pr.IllResponse.DeliveryInfo, repo.savedPr.IllResponse.DeliveryInfo)
			assert.Equal(t, pr.IllResponse.ReturnInfo, repo.savedPr.IllResponse.ReturnInfo)
			assert.Empty(t, repo.savedItems)
		})
	}
}

func TestSupplierOverdueAndRenewalSendBeforeTransition(t *testing.T) {
	for _, action := range []pr_db.PatronRequestAction{LenderActionOverdue, LenderActionAcceptRenewal, LenderActionRejectRenewal} {
		t.Run(string(action), func(t *testing.T) {
			pr := testLoan()
			pr.State = LenderStateReceived
			if action != LenderActionOverdue {
				pr.State = LenderStateRenewalPending
			}
			old := time.Now().UTC().Add(-time.Hour)
			pr.DueAt = pgtype.Timestamptz{Time: old, Valid: true}
			pr.IllResponse.DeliveryInfo = &iso18626.DeliveryInfo{ItemId: "shipment"}
			pr.IllResponse.ReturnInfo = &iso18626.ReturnInfo{PhysicalAddress: &iso18626.PhysicalAddress{}}
			pr.IllResponse.StatusInfo.DueDate = isoLoanDate(pr.DueAt)
			pr.IllResponse.MessageInfo = iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange, AnswerYesNo: loanYesNo(iso18626.TypeYesNoN), Note: "old note"}
			repo := &MockPrRepo{savedPr: pr}
			sender := &MockIso18626Handler{failSupplyingAgencyMessage: true}
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, nil, nil, nil, nil)
			event := events.Event{ID: "event", PatronRequestID: pr.ID, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: map[string]any{"dueDate": "2030-01-01", "note": "renewal decision"}}}
			status, _ := svc.handleInvokeAction(appCtx, event)
			require.NotEqual(t, events.EventStatusSuccess, status)
			assert.Equal(t, pr.State, repo.savedPr.State)
			assert.Equal(t, old, repo.savedPr.DueAt.Time)
			assert.True(t, repo.savedPr.NeedsAttention)
			assert.Equal(t, pr.IllResponse, repo.savedPr.IllResponse, "failed send must retain the prior supplier status")
			sender.failSupplyingAgencyMessage = false
			status, _ = svc.handleInvokeAction(appCtx, event)
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, sender.lastSupplyingAgencyMessage.StatusInfo, repo.savedPr.IllResponse.StatusInfo)
			assert.Equal(t, sender.lastSupplyingAgencyMessage.MessageInfo, repo.savedPr.IllResponse.MessageInfo)
			assert.Equal(t, pr.IllResponse.DeliveryInfo, repo.savedPr.IllResponse.DeliveryInfo)
			assert.Equal(t, pr.IllResponse.ReturnInfo, repo.savedPr.IllResponse.ReturnInfo)
			if action == LenderActionAcceptRenewal {
				assert.Equal(t, LenderStateRenewed, repo.savedPr.State)
				assert.True(t, repo.savedPr.DueAt.Time.After(old))
				assert.Nil(t, sender.lastSupplyingAgencyMessage.DeliveryInfo)
				assert.Equal(t, iso18626.TypeStatusLoaned, sender.lastSupplyingAgencyMessage.StatusInfo.Status)
				assert.Equal(t, iso18626.TypeReasonForMessageRenewResponse, sender.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
			} else {
				assert.Equal(t, LenderStateOverdue, repo.savedPr.State)
			}
		})
	}
}

func TestSupplierOverdueUsesConfiguredStateAndRechecksDueDate(t *testing.T) {
	for _, tc := range []struct {
		name string
		due  pgtype.Timestamptz
		want events.EventStatus
	}{
		{name: "past due", due: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}, want: events.EventStatusSuccess},
		{name: "future due", due: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, want: events.EventStatusError},
		{name: "no due date", want: events.EventStatusError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := testLoan()
			pr.State = "CUSTOM_RECEIVED"
			pr.DueAt = tc.due
			repo := &MockPrRepo{savedPr: pr}
			sender := new(MockIso18626Handler)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, nil, nil, nil, nil)
			model, err := svc.actionMappingService.GetStateModel("default")
			require.NoError(t, err)
			for _, state := range model.States {
				if state.Name == string(LenderStateReceived) && state.Side == "SUPPLIER" {
					state.Name = string(pr.State)
					model.States = append(model.States, state)
					break
				}
			}
			action := LenderActionOverdue
			event := events.Event{ID: "event", PatronRequestID: pr.ID, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}}
			status, result := svc.handleInvokeAction(appCtx, event)
			require.Equal(t, tc.want, status, "%+v", result)
			if tc.want == events.EventStatusSuccess {
				assert.Equal(t, LenderStateOverdue, repo.savedPr.State)
				require.NotNil(t, sender.lastSupplyingAgencyMessage)
				assert.Equal(t, iso18626.TypeStatusOverdue, sender.lastSupplyingAgencyMessage.StatusInfo.Status)
			} else {
				assert.Equal(t, pr.State, repo.savedPr.State)
				assert.Nil(t, sender.lastSupplyingAgencyMessage)
			}
		})
	}
}

func TestRequesterHelpersRepeatAllItemsWithoutChangingLoanState(t *testing.T) {
	for _, state := range []pr_db.PatronRequestState{BorrowerStateReceived, BorrowerStateRenewed, BorrowerStateOverdue, BorrowerStateRenewalPending} {
		repo := &MockPrRepo{savedPr: testLoan(), savedItems: []pr_db.Item{{ID: "a", PrID: "loan", Barcode: "a", LmsStatus: pr_db.LmsStatusCheckedOut}, {ID: "b", PrID: "loan", Barcode: "b", LmsStatus: pr_db.LmsStatusUnknown}}}
		repo.savedPr.Side = SideBorrowing
		repo.savedPr.State = state
		adapter := new(mockLmsAdapter)
		patronDue := time.Now().Add(time.Hour)
		adapter.On("CheckOutItem", "loan", "a", "", "externalReferenceValue").Return(&lms.CheckedOutItem{DueDate: &patronDue}, nil).Twice()
		adapter.On("CheckOutItem", "loan", "b", "", "externalReferenceValue").Return(nil, errors.New("failed")).Once()
		adapter.On("CheckOutItem", "loan", "b", "", "externalReferenceValue").Return(&lms.CheckedOutItem{}, nil).Once()
		creator := new(MockLmsCreator)
		creator.On("GetAdapter", "ISIL:REQ").Return(adapter, nil)
		svc := CreatePatronRequestActionService(repo, nil, new(MockEventBus), new(MockIso18626Handler), creator, nil, nil, nil)
		action := BorrowerActionCheckOut
		event := events.Event{ID: "helper", PatronRequestID: "loan", EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}}
		status, _ := svc.handleInvokeAction(appCtx, event)
		require.Equal(t, events.EventStatusError, status)
		status, _ = svc.handleInvokeAction(appCtx, event)
		require.Equal(t, events.EventStatusSuccess, status)
		assert.Equal(t, state, repo.savedPr.State)
		assert.False(t, repo.savedPr.DueAt.Valid)
		assert.False(t, repo.savedItems[0].LmsDueDate.Valid)
		adapter.AssertExpectations(t)
	}
}

func TestSkippedCheckOutPreservesItemProgress(t *testing.T) {
	for _, side := range []pr_db.PatronRequestSide{SideBorrowing, SideLending} {
		t.Run(string(side), func(t *testing.T) {
			pr := testLoan()
			pr.Side = side
			if side == SideBorrowing {
				pr.State = BorrowerStateReceived
			}
			repo := &MockPrRepo{savedPr: pr, savedItems: []pr_db.Item{{ID: "a", PrID: pr.ID, Barcode: "a", LmsStatus: pr_db.LmsStatusRequested}}}
			adapter := new(mockLmsAdapter)
			adapter.On("CheckOutItem", mock.Anything, "a", mock.Anything, mock.Anything).Return(nil, nil).Once()
			bus := new(MockEventBus)
			sender := new(MockIso18626Handler)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), bus, sender, nil, nil, nil, nil)
			var result actionExecutionResult
			if side == SideLending {
				result = svc.shipLenderRequest(appCtx, "event", pr, adapter, pr.IllRequest, actionParams{DueDate: "2030-01-01"})
				require.True(t, result.pr.DueAt.Valid, "skipped checkout must still allow the manual due date")
			} else {
				result = svc.checkoutBorrowingRequest(appCtx, pr, adapter, pr.IllRequest)
				assert.Nil(t, sender.lastSupplyingAgencyMessage)
			}
			require.Equal(t, events.EventStatusSuccess, result.status)
			assert.Equal(t, pr_db.LmsStatusRequested, repo.savedItems[0].LmsStatus)
			assert.False(t, repo.savedItems[0].LmsDueDate.Valid)
			if side == SideLending {
				assert.Equal(t, []events.EventName{events.EventNameIllSupplierMessage}, bus.createdNoticeNames)
			} else {
				assert.Empty(t, bus.createdNoticeData)
			}
			adapter.AssertExpectations(t)
		})
	}
}

func TestSkippedLmsOperationPreservesKnownStatus(t *testing.T) {
	repo := &MockPrRepo{savedItems: []pr_db.Item{{ID: "a", PrID: "loan", LmsStatus: pr_db.LmsStatusCheckedOut}}}
	bus := new(MockEventBus)
	svc := CreatePatronRequestActionService(repo, nil, bus, nil, nil, nil, nil, nil)
	require.NoError(t, svc.recordItemLmsStatus(appCtx, repo.savedItems[0], pr_db.LmsStatusCheckedIn, false, nil))
	assert.Equal(t, pr_db.LmsStatusCheckedOut, repo.savedItems[0].LmsStatus)
	assert.Empty(t, bus.createdNoticeData)
}

func TestOverdueEventsAndTransitionlessHelpers(t *testing.T) {
	mapping := mustActionMapping(t)
	for _, state := range []pr_db.PatronRequestState{BorrowerStateReceived, BorrowerStateRenewed, BorrowerStateOverdue, BorrowerStateRenewalPending} {
		t.Run(string(state), func(t *testing.T) {
			repo := new(MockPrRepo)
			pr := testLoan()
			pr.Side = SideBorrowing
			pr.State = state
			for _, action := range []pr_db.PatronRequestAction{BorrowerActionCheckIn, BorrowerActionCheckOut, BorrowerActionShipReturn} {
				assert.True(t, mapping.IsActionSupported(pr, action))
			}
			assert.Equal(t, state == BorrowerStateOverdue, mapping.IsActionSupported(pr, BorrowerActionRenew))
			handler := CreatePatronRequestMessageHandler(repo, nil, nil, nil)
			status, _, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{MessageInfo: iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusOverdue}}, pr)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusSuccess, status)
			if state == BorrowerStateRenewalPending {
				assert.Equal(t, state, repo.savedPr.State)
			} else {
				assert.Equal(t, BorrowerStateOverdue, repo.savedPr.State)
			}
			assert.False(t, repo.savedPr.DueAt.Valid, "supplier signal also applies to open-ended loans")
		})
	}
}
