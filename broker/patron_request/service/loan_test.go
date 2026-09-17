package prservice

import (
	"errors"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
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
	earlierManual := now.AddDate(0, 0, 1)
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
		{name: "manual overrides checkout and default", items: []pr_db.Item{
			{LmsDueDate: pgtype.Timestamptz{Time: later, Valid: true}},
			{LmsDueDate: pgtype.Timestamptz{Time: earliest, Valid: true}},
		}, manual: &manual, entry: entry, want: manual, source: "ship.dueDate"},
		{name: "earlier manual overrides checkout", items: []pr_db.Item{
			{LmsDueDate: pgtype.Timestamptz{Time: earliest, Valid: true}},
		}, manual: &earlierManual, entry: entry, want: earlierManual, source: "ship.dueDate"},
		{name: "manual overrides default", manual: &manual, entry: entry, want: manual, source: "ship.dueDate"},
		{name: "earliest checkout overrides default", items: []pr_db.Item{
			{LmsDueDate: pgtype.Timestamptz{Time: later, Valid: true}},
			{LmsDueDate: pgtype.Timestamptz{Time: earliest, Valid: true}},
		}, entry: entry, want: earliest, source: "LMS checkout"},
		{name: "invalid checkout dates ignored", items: []pr_db.Item{
			{LmsDueDate: pgtype.Timestamptz{Time: earliest}},
			{LmsDueDate: pgtype.Timestamptz{Valid: true}},
		}, entry: entry, want: time.Date(2026, 3, 30, 21, 59, 59, 0, time.UTC), source: "illConfig.defaultLoanPeriod"},
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
	result := svc.shipLenderRequest(appCtx, "event", repo.savedPr, adapter, repo.savedPr.IllRequest, actionParams{DueDate: ptr("2030-01-01")})
	assert.Equal(t, events.EventStatusError, result.status)
	assert.False(t, result.pr.DueAt.Valid, "fallback must not finalize before all checkouts")
	assert.Equal(t, pr_db.LmsStatusCheckedOut, repo.savedItems[0].LmsStatus)
	assert.Equal(t, pr_db.LmsStatusRequested, repo.savedItems[1].LmsStatus)
	assert.Nil(t, sender.lastSupplyingAgencyMessage)
	failed.Unset()
	adapter.On("CheckOutItem", "", "b", "", "").Return(&lms.CheckedOutItem{DueDate: &earliest}, nil).Once()
	result = svc.shipLenderRequest(appCtx, "retry", result.pr, adapter, result.pr.IllRequest, actionParams{DueDate: ptr("2030-01-01")})
	require.Equal(t, events.EventStatusSuccess, result.status)
	manual := time.Date(2030, 1, 1, 23, 59, 59, 0, time.UTC)
	assert.Equal(t, manual, result.pr.DueAt.Time)
	assert.Equal(t, manual, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate.Time)
	// A delivery retry uses the current manual date, not the previous request date.
	result.pr.DueAt = pgtype.Timestamptz{Time: first, Valid: true}
	result = svc.shipLenderRequest(appCtx, "retry-delivery", result.pr, adapter, result.pr.IllRequest, actionParams{DueDate: ptr("2031-01-01")})
	require.Equal(t, events.EventStatusSuccess, result.status)
	manual = time.Date(2031, 1, 1, 23, 59, 59, 0, time.UTC)
	assert.Equal(t, manual, result.pr.DueAt.Time)
	assert.Equal(t, manual, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate.Time)
	// Without a manual date, fall back to the stored checkout dates.
	result = svc.shipLenderRequest(appCtx, "retry-without-manual", result.pr, adapter, result.pr.IllRequest, actionParams{})
	require.Equal(t, events.EventStatusSuccess, result.status)
	assert.Equal(t, earliest, result.pr.DueAt.Time)
	assert.Equal(t, earliest, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate.Time)
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

func TestShippingRetryRecalculatesDueDate(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		initialDays, retryDays     int32
		initialManual, retryManual *string
	}{
		{name: "new default", retryDays: 14},
		{name: "changed default", initialDays: 14, retryDays: 28},
		{name: "removed default", initialDays: 14},
		{name: "changed manual date", initialManual: ptr("2030-01-01"), retryManual: ptr("2031-01-01")},
		{name: "removed manual date", initialManual: ptr("2030-01-01")},
		{name: "still open-ended"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := testLoan()
			repo := &MockPrRepo{savedPr: pr, savedItems: []pr_db.Item{{ID: "item", PrID: pr.ID, Barcode: "item"}}}
			directory := new(IllRepoMock)
			entry := dirapi.Entry{IllConfig: &dirapi.IllConfig{}}
			if tc.initialDays > 0 {
				entry.IllConfig.DefaultLoanPeriod.Set(tc.initialDays)
			}
			initialLookup := directory.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything).Return([]ill_db.Peer{{CustomData: entry}}, "", nil)
			sender := &MockIso18626Handler{failSupplyingAgencyMessage: true}
			adapter := new(mockLmsAdapter)
			adapter.On("CheckOutItem", "", "item", "", "").Return(&lms.CheckedOutItem{}, nil).Once()
			creator := new(MockLmsCreator)
			creator.On("GetAdapter", pr.SupplierSymbol.String).Return(adapter, nil)
			svc := CreatePatronRequestActionService(repo, directory, new(MockEventBus), sender, creator, nil, nil, nil)
			action := LenderActionShip
			event := events.Event{ID: "ship", PatronRequestID: pr.ID, EventData: events.EventData{
				CommonEventData: events.CommonEventData{Action: &action},
				CustomData:      map[string]any{"dueDate": tc.initialManual},
			}}
			status, _ := svc.handleInvokeAction(appCtx, event)
			require.NotEqual(t, events.EventStatusSuccess, status)
			assert.Equal(t, tc.initialDays > 0 || tc.initialManual != nil, repo.savedPr.DueAt.Valid)
			assert.Equal(t, pr.State, repo.savedPr.State)

			initialLookup.Unset()
			entry = dirapi.Entry{IllConfig: &dirapi.IllConfig{}}
			if tc.retryDays > 0 {
				entry.IllConfig.DefaultLoanPeriod.Set(tc.retryDays)
			}
			directory.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything).Return([]ill_db.Peer{{CustomData: entry}}, "", nil)
			event.EventData.CustomData = map[string]any{"dueDate": tc.retryManual}
			sender.failSupplyingAgencyMessage = false
			status, _ = svc.handleInvokeAction(appCtx, event)
			require.Equal(t, events.EventStatusSuccess, status)
			var want *time.Time
			var err error
			if tc.retryManual != nil {
				want, err = parseLoanDate(*tc.retryManual, entry)
			} else {
				want, err = defaultLoanDate(entry, time.Now())
			}
			require.NoError(t, err)
			assert.Equal(t, want != nil, repo.savedPr.DueAt.Valid)
			if want != nil {
				require.NotNil(t, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate)
				assert.True(t, want.Equal(repo.savedPr.DueAt.Time))
				assert.True(t, want.Equal(sender.lastSupplyingAgencyMessage.StatusInfo.DueDate.Time))
			} else {
				assert.Nil(t, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate)
				assert.Nil(t, repo.savedPr.IllResponse.StatusInfo.DueDate)
			}
			adapter.AssertExpectations(t)
		})
	}
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
				params.DueDate, source = ptr("2030-01-01"), "ship.dueDate"
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
			// Each retry resolves and audits the date using its current inputs.
			sender.failSupplyingAgencyMessage = false
			retry := svc.shipLenderRequest(appCtx, "retry", result.pr, &lms.LmsAdapterManual{}, pr.IllRequest, actionParams{DueDate: ptr("2031-01-01")})
			require.Equal(t, events.EventStatusSuccess, retry.status)
			want, err := parseLoanDate("2031-01-01", entry)
			require.NoError(t, err)
			assert.True(t, want.Equal(retry.pr.DueAt.Time))
			retryResolution := retry.result.CustomData["dueDateResolution"].(map[string]any)
			assert.Equal(t, "ship.dueDate", retryResolution["source"])
		}
	}
}

func TestIncomingLoanAndRenewalDates(t *testing.T) {
	old := time.Now().UTC().AddDate(0, 0, -30)
	past := old.AddDate(0, 0, 1)
	for _, tc := range []struct {
		name         string
		state        pr_db.PatronRequestState
		reason       iso18626.TypeReasonForMessage
		answer       *iso18626.TypeYesNo
		date         *utils.XSDDateTime
		want         pr_db.PatronRequestState
		fail         bool
		existingDate bool
	}{
		{name: "open-ended shipment", state: BorrowerStateWillSupply, reason: iso18626.TypeReasonForMessageStatusChange, want: BorrowerStateShipped},
		{name: "accept past date without local overdue decision", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), date: &utils.XSDDateTime{Time: past}, want: BorrowerStateRenewed},
		{name: "undated acceptance clears previous date", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), want: BorrowerStateRenewed},
		{name: "zero renewal date clears previous date", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), date: &utils.XSDDateTime{}, want: BorrowerStateRenewed},
		{name: "zero shipment date preserves previous date", state: BorrowerStateWillSupply, reason: iso18626.TypeReasonForMessageStatusChange, date: &utils.XSDDateTime{}, want: BorrowerStateShipped, existingDate: true},
		{name: "reject preserves date", state: BorrowerStateRenewalPending, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoN), want: BorrowerStateOverdue},
		{name: "unsolicited response", state: BorrowerStateReceived, reason: iso18626.TypeReasonForMessageRenewResponse, answer: loanYesNo(iso18626.TypeYesNoY), date: &utils.XSDDateTime{Time: past}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(MockPrRepo)
			handler := CreatePatronRequestMessageHandler(repo, nil, nil, nil)
			pr := testLoan()
			pr.Side = SideBorrowing
			pr.State = tc.state
			if tc.reason == iso18626.TypeReasonForMessageRenewResponse || tc.existingDate {
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
					if tc.date == nil || tc.date.IsZero() {
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
			} else if tc.existingDate {
				assert.True(t, repo.savedPr.DueAt.Valid)
				assert.Equal(t, old, repo.savedPr.DueAt.Time)
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

func TestRequesterRenewDispatch(t *testing.T) {
	pr := testLoan()
	pr.Side, pr.State = SideBorrowing, BorrowerStateOverdue
	repo := &MockPrRepo{savedPr: pr}
	sender := new(MockIso18626Handler)
	creator := new(MockLmsCreator)
	creator.On("GetAdapter", pr.RequesterSymbol.String).Return(&lms.LmsAdapterManual{}, nil)
	svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, creator, nil, nil, nil)
	action := BorrowerActionRenew
	event := events.Event{ID: "event", PatronRequestID: pr.ID, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action}, CustomData: map[string]any{"note": "please renew"},
	}}
	status, _ := svc.handleInvokeAction(appCtx, event)
	require.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, BorrowerStateRenewalPending, repo.savedPr.State)
	require.NotNil(t, sender.lastRequestingAgencyMessage)
	assert.Equal(t, iso18626.TypeActionRenew, sender.lastRequestingAgencyMessage.Action)
	assert.Equal(t, "please renew", sender.lastRequestingAgencyMessage.Note)
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
			creator := new(MockLmsCreator)
			creator.On("GetAdapter", pr.SupplierSymbol.String).Return(&lms.LmsAdapterManual{}, nil)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, creator, nil, nil, nil)
			futureDue := time.Now().UTC().AddDate(0, 0, 14).Format(time.RFC3339)
			event := events.Event{ID: "event", PatronRequestID: pr.ID, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: map[string]any{"dueDate": futureDue, "note": "renewal decision"}}}
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

func TestActionDueDatePointer(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
		want *string
	}{
		{name: "omitted"},
		{name: "null", data: map[string]any{"dueDate": nil}},
		{name: "empty", data: map[string]any{"dueDate": ""}, want: ptr("")},
		{name: "supplied", data: map[string]any{"dueDate": "2030-01-01"}, want: ptr("2030-01-01")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var params actionParams
			require.NoError(t, common.MapToStruct(tc.data, &params))
			assert.Equal(t, tc.want, params.DueDate)
		})
	}
}

func TestLoanHandlersRejectInvalidDueDateParams(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "empty", value: ""},
		{name: "blank", value: " "},
		{name: "wrong type", value: 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := testLoan()
			svc := &PatronRequestActionService{}
			var params actionParams
			err := common.MapToStruct(map[string]any{"dueDate": tc.value}, &params)
			if tc.name == "wrong type" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			// Validation must run before directory, LMS, or message-sending calls.
			for _, result := range []actionExecutionResult{
				svc.shipLenderRequest(appCtx, "event", pr, nil, pr.IllRequest, params),
				svc.renewalLenderRequest(appCtx, "event", pr, params, true),
			} {
				assert.Equal(t, events.EventStatusError, result.status)
				assert.Equal(t, pr, result.pr)
				require.NotNil(t, result.result.EventError)
				assert.Equal(t, "supplied dueDate must be a non-empty date or RFC3339 timestamp", result.result.EventError.Message)
			}
		})
	}
}

func TestSupplierRenewalOptionalDueDate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		params     map[string]any
		failedSend bool
		fail       bool
	}{
		{name: "omitted date creates open-ended renewal"},
		{name: "failed send retains previous date", failedSend: true, fail: true},
		{name: "empty date", params: map[string]any{"dueDate": ""}, fail: true},
		{name: "blank date", params: map[string]any{"dueDate": " "}, fail: true},
		{name: "null date creates open-ended renewal", params: map[string]any{"dueDate": nil}},
		{name: "wrong type", params: map[string]any{"dueDate": 42}, fail: true},
		{name: "malformed date", params: map[string]any{"dueDate": "invalid"}, fail: true},
		{name: "past date", params: map[string]any{"dueDate": "2000-01-01T00:00:00Z"}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := testLoan()
			pr.State = LenderStateRenewalPending
			pr.DueAt = pgtype.Timestamptz{Time: time.Now().UTC().Add(-time.Hour), Valid: true}
			pr.IllResponse.StatusInfo.DueDate = isoLoanDate(pr.DueAt)
			repo := &MockPrRepo{savedPr: pr}
			sender := &MockIso18626Handler{failSupplyingAgencyMessage: tc.failedSend}
			creator := new(MockLmsCreator)
			creator.On("GetAdapter", pr.SupplierSymbol.String).Return(&lms.LmsAdapterManual{}, nil)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, creator, nil, nil, nil)
			action := LenderActionAcceptRenewal
			event := events.Event{ID: "event", PatronRequestID: pr.ID, EventData: events.EventData{
				CommonEventData: events.CommonEventData{Action: &action}, CustomData: tc.params,
			}}
			status, _ := svc.handleInvokeAction(appCtx, event)
			if tc.fail {
				require.NotEqual(t, events.EventStatusSuccess, status)
				assert.Equal(t, LenderStateRenewalPending, repo.savedPr.State)
				assert.Equal(t, pr.DueAt, repo.savedPr.DueAt)
				assert.Equal(t, pr.IllResponse, repo.savedPr.IllResponse)
				if !tc.failedSend {
					assert.Nil(t, sender.lastSupplyingAgencyMessage)
				}
				return
			}
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, LenderStateRenewed, repo.savedPr.State)
			assert.Equal(t, pgtype.Timestamptz{}, repo.savedPr.DueAt)
			assert.Nil(t, repo.savedPr.IllResponse.StatusInfo.DueDate)
			require.NotNil(t, sender.lastSupplyingAgencyMessage)
			assert.Nil(t, sender.lastSupplyingAgencyMessage.StatusInfo.DueDate)
			assert.Equal(t, iso18626.TypeStatusLoaned, sender.lastSupplyingAgencyMessage.StatusInfo.Status)
			assert.Equal(t, iso18626.TypeReasonForMessageRenewResponse, sender.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
			assert.Equal(t, loanYesNo(iso18626.TypeYesNoY), sender.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo)
		})
	}
}

func TestSupplierOverdueUsesConfiguredStateAndRechecksDueDate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		due         pgtype.Timestamptz
		state       pr_db.PatronRequestState
		serviceType iso18626.TypeServiceType
		snapshot    iso18626.TypeStatus
		want        events.EventStatus
	}{
		{name: "past due", due: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}, want: events.EventStatusSuccess},
		{name: "future due", due: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, want: events.EventStatusError},
		{name: "no due date", want: events.EventStatusSuccess},
		{name: "snapshot does not control eligibility", due: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}, snapshot: iso18626.TypeStatusCopyCompleted, want: events.EventStatusSuccess},
		{name: "completed copy or loan cannot become overdue", due: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}, state: LenderStateCompleted, serviceType: iso18626.TypeServiceTypeCopyOrLoan, want: events.EventStatusError},
		{name: "copy cannot become overdue", due: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}, serviceType: iso18626.TypeServiceTypeCopy, want: events.EventStatusError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := testLoan()
			pr.State = "CUSTOM_RECEIVED"
			pr.DueAt = tc.due
			if tc.state != "" {
				pr.State = tc.state
			}
			if tc.serviceType != "" {
				pr.IllRequest.ServiceInfo.ServiceType = tc.serviceType
			}
			pr.IllResponse.StatusInfo.Status = tc.snapshot
			repo := &MockPrRepo{savedPr: pr}
			sender := new(MockIso18626Handler)
			creator := new(MockLmsCreator)
			creator.On("GetAdapter", pr.SupplierSymbol.String).Return(&lms.LmsAdapterManual{}, nil)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, creator, nil, nil, nil)
			model, err := svc.actionMappingService.GetStateModel("default")
			require.NoError(t, err)
			for _, state := range model.States {
				if state.Name == string(LenderStateReceived) && state.Side == "SUPPLIER" {
					state.Name = "CUSTOM_RECEIVED"
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
				assert.Equal(t, tc.due, repo.savedPr.DueAt)
				assert.Equal(t, isoLoanDate(tc.due), sender.lastSupplyingAgencyMessage.StatusInfo.DueDate)
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
				result = svc.shipLenderRequest(appCtx, "event", pr, adapter, pr.IllRequest, actionParams{DueDate: ptr("2030-01-01")})
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

func TestIncomingOverdueDueDate(t *testing.T) {
	past := time.Now().UTC().Add(-time.Hour)
	future := past.Add(48 * time.Hour)
	old := pgtype.Timestamptz{Time: past.Add(-time.Hour), Valid: true}
	for _, tc := range []struct {
		name    string
		initial pgtype.Timestamptz
		date    *utils.XSDDateTime
		want    pgtype.Timestamptz
	}{
		{name: "provided date replaces existing", initial: old, date: &utils.XSDDateTime{Time: past}, want: pgtype.Timestamptz{Time: past, Valid: true}},
		{name: "provided date dates open-ended loan", date: &utils.XSDDateTime{Time: past}, want: pgtype.Timestamptz{Time: past, Valid: true}},
		{name: "future date trusts supplier", initial: old, date: &utils.XSDDateTime{Time: future}, want: pgtype.Timestamptz{Time: future, Valid: true}},
		{name: "omitted date preserves existing", initial: old, want: old},
		{name: "omitted date preserves open-ended loan"},
		{name: "zero date preserves existing", initial: old, date: &utils.XSDDateTime{}, want: old},
		{name: "zero date preserves open-ended loan", date: &utils.XSDDateTime{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := testLoan()
			pr.Side, pr.State = SideBorrowing, BorrowerStateReceived
			pr.DueAt = tc.initial
			repo := &MockPrRepo{savedPr: pr}
			handler := CreatePatronRequestMessageHandler(repo, nil, nil, nil)
			sam := iso18626.SupplyingAgencyMessage{
				MessageInfo: iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange},
				StatusInfo:  iso18626.StatusInfo{Status: iso18626.TypeStatusOverdue, DueDate: tc.date},
			}
			status, _, err := handler.handleSupplyingAgencyMessage(appCtx, sam, pr)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, BorrowerStateOverdue, repo.savedPr.State)
			assert.Equal(t, tc.want, repo.savedPr.DueAt)
			assert.Equal(t, isoLoanDate(tc.want), repo.savedPr.IllResponse.StatusInfo.DueDate)
		})
	}
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
