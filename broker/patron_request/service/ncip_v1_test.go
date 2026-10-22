package prservice

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	ncip "github.com/indexdata/crosslink/ncipv1"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNcipV1RequesterLifecycle(t *testing.T) {
	operations := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message ncip.NCIPMessage
		require.NoError(t, xml.NewDecoder(r.Body).Decode(&message))
		operation := ""
		switch {
		case message.AcceptItem != nil:
			operation = "AcceptItem"
		case message.CheckOutItem != nil:
			operation = "CheckOutItem"
			require.Equal(t, "patron", message.CheckOutItem.UniqueUserId.UserIdentifierValue)
		case message.CheckInItem != nil:
			operation = "CheckInItem"
		default:
			t.Errorf("unexpected v1 service")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		operations <- operation
		data, err := os.ReadFile("../../ncipclientv1/testdata/" + operation + "Response.xml")
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/xml")
		_, err = w.Write(data)
		require.NoError(t, err)
	}))
	defer server.Close()
	version := "1.0"
	adapter, err := lms.CreateLmsAdapterNcip(dirapi.LmsConfig{Address: server.URL, FromAgency: "broker", NcipVersion: &version})
	require.NoError(t, err)
	pr := testLoan()
	pr.Side = SideBorrowing
	pr.State = BorrowerStateShipped
	pr.Patron = getDbText("patron")
	repo := &MockPrRepo{savedPr: pr, savedItems: []pr_db.Item{{ID: "item", PrID: pr.ID, Barcode: "barcode", LmsStatus: pr_db.LmsStatusUnknown}}}
	bus := new(MockEventBus)
	sender := new(MockIso18626Handler)
	creator := new(MockLmsCreator)
	creator.On("GetAdapter", pr.RequesterSymbol.String).Return(adapter, nil)
	svc := CreatePatronRequestActionService(repo, new(IllRepoMock), bus, sender, creator, nil, nil, nil)
	adapter, err = svc.createRequesterLmsAdapter(appCtx, "receive", pr)
	require.NoError(t, err)
	result := svc.receiveBorrowingRequest(appCtx, "receive", pr, adapter, pr.IllRequest)
	require.Equal(t, events.EventStatusSuccess, result.status, "%+v", result.result)
	require.Equal(t, pr_db.LmsStatusAccepted, repo.savedItems[0].LmsStatus)
	require.True(t, repo.savedItems[0].LmsRequestID.Valid)
	result = svc.checkoutBorrowingRequest(appCtx, pr, adapter, pr.IllRequest)
	require.Equal(t, events.EventStatusSuccess, result.status, "%+v", result.result)
	require.Equal(t, pr_db.LmsStatusCheckedOut, repo.savedItems[0].LmsStatus)
	result = svc.checkinBorrowingRequest(appCtx, pr, adapter, pr.IllRequest)
	require.Equal(t, events.EventStatusSuccess, result.status, "%+v", result.result)
	require.Equal(t, pr_db.LmsStatusCheckedIn, repo.savedItems[0].LmsStatus)
	before := repo.savedItems[0]
	result = svc.shipReturnBorrowingRequest(appCtx, "return", pr, adapter, pr.IllRequest)
	require.Equal(t, events.EventStatusSuccess, result.status, "%+v", result.result)
	require.Equal(t, iso18626.TypeActionShippedReturn, sender.lastRequestingAgencyMessage.Action)
	require.Equal(t, before, repo.savedItems[0], "unsupported cleanup must retain status and LMS IDs")
	// Local-close cleanup uses the same unsupported-service handling.
	require.NoError(t, svc.deleteRequesterItemsOnClose(appCtx, "close", pr))
	require.Equal(t, before, repo.savedItems[0])
	require.Equal(t, []string{"AcceptItem", "CheckOutItem", "CheckInItem"}, []string{<-operations, <-operations, <-operations})
	skipped := 0
	for i, data := range bus.createdNoticeData {
		if data.CustomData["outcome"] != "skipped" {
			continue
		}
		skipped++
		require.Equal(t, events.EventNameLmsRequesterMessage, bus.createdNoticeNames[i])
		require.Equal(t, events.EventStatusProblem, bus.createdNoticeStatus[i])
		require.Equal(t, map[string]any{"operation": "DeleteItem", "outcome": "skipped", "reason": "unsupported", "itemId": "barcode"}, data.CustomData)
		require.Contains(t, []string{"return", "close"}, bus.createdNoticeParent[i])
	}
	require.Equal(t, 2, skipped)
}

func TestRequesterCleanupFailuresStillBlockReturn(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		t.Run(map[bool]string{false: "deletion failure", true: "audit failure"}[unsupported], func(t *testing.T) {
			pr := testLoan()
			pr.Side = SideBorrowing
			item := pr_db.Item{ID: "item", PrID: pr.ID, Barcode: "barcode", LmsRequestID: getDbText("request"), LmsItemID: getDbText("barcode"), LmsStatus: pr_db.LmsStatusCheckedIn}
			repo := &MockPrRepo{savedPr: pr, savedItems: []pr_db.Item{item}}
			bus := new(MockEventBus)
			sender := new(MockIso18626Handler)
			adapter := new(mockLmsAdapter)
			failure := errors.New("LMS unavailable")
			if unsupported {
				failure = lms.ErrDeleteItemUnsupported
				bus.On("CreateNoticeWithParent", "return").Return("", errors.New("audit unavailable"))
			}
			adapter.On("DeleteItem", "barcode").Return(failure).Once()
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), bus, sender, nil, nil, nil, nil)
			result := svc.shipReturnBorrowingRequest(appCtx, "return", pr, adapter, pr.IllRequest)
			require.Equal(t, events.EventStatusError, result.status)
			require.Nil(t, sender.lastRequestingAgencyMessage)
			require.Equal(t, item, repo.savedItems[0])
			adapter.AssertExpectations(t)
		})
	}
}

func TestIncompleteNcipV1ResponseDoesNotRecordProgress(t *testing.T) {
	for _, operation := range []string{"AcceptItem", "CheckOutItem", "CheckInItem"} {
		t.Run(operation, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				_, err := w.Write([]byte("<NCIPMessage><" + operation + "Response/></NCIPMessage>"))
				require.NoError(t, err)
			}))
			defer server.Close()
			adapter, err := lms.CreateLmsAdapterNcip1(dirapi.LmsConfig{Address: server.URL, FromAgency: "broker"})
			require.NoError(t, err)
			pr := testLoan()
			pr.Side = SideBorrowing
			pr.Patron = getDbText("patron")
			item := pr_db.Item{ID: "item", PrID: pr.ID, Barcode: "barcode", LmsStatus: pr_db.LmsStatusUnknown}
			repo := &MockPrRepo{savedPr: pr, savedItems: []pr_db.Item{item}}
			sender := new(MockIso18626Handler)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), sender, nil, nil, nil, nil)
			var result actionExecutionResult
			switch operation {
			case "AcceptItem":
				result = svc.receiveBorrowingRequest(appCtx, "receive", pr, adapter, pr.IllRequest)
			case "CheckOutItem":
				result = svc.checkoutBorrowingRequest(appCtx, pr, adapter, pr.IllRequest)
			case "CheckInItem":
				result = svc.checkinBorrowingRequest(appCtx, pr, adapter, pr.IllRequest)
			}
			require.Equal(t, events.EventStatusError, result.status)
			require.Equal(t, item, repo.savedItems[0])
			require.Nil(t, sender.lastRequestingAgencyMessage)
		})
	}
}

func ncipV1ResponseFixture(t *testing.T, operation string) ncip.NCIPMessage {
	t.Helper()
	data, err := os.ReadFile("../../ncipclientv1/testdata/" + operation + "Response.xml")
	require.NoError(t, err)
	var message ncip.NCIPMessage
	require.NoError(t, xml.Unmarshal(data, &message))
	return message
}

func TestNcipV1PatronRejection(t *testing.T) {
	for _, transportFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "protocol rejection", true: "HTTP failure"}[transportFailure], func(t *testing.T) {
			var calls atomic.Int32
			response := ncipV1ResponseFixture(t, "LookupUser")
			response.LookupUserResponse.UniqueUserId = nil
			response.LookupUserResponse.Problem = &ncip.Problem{ProcessingError: []ncip.ProcessingError{{
				ProcessingErrorType:    ncip.ProcessingErrorType{Scheme: "urn:test:error", Value: "Unknown User"},
				ProcessingErrorElement: ncip.ProcessingErrorElement{ElementName: "UniqueUserId", ProcessingErrorValue: "secret-password"},
			}}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if transportFailure {
					w.WriteHeader(http.StatusUnauthorized)
					_, err := w.Write([]byte("secret-password"))
					require.NoError(t, err)
					return
				}
				w.Header().Set("Content-Type", "application/xml")
				require.NoError(t, xml.NewEncoder(w).Encode(response))
			}))
			defer server.Close()
			adapter, err := lms.CreateLmsAdapterNcip1(dirapi.LmsConfig{Address: server.URL, FromAgency: "broker"})
			require.NoError(t, err)
			pr := pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateNew, Side: SideBorrowing, RequesterSymbol: getDbText("ISIL:REC1"), Patron: getDbText("patron")}
			repo := &MockPrRepo{savedPr: pr}
			bus := new(MockEventBus)
			creator := new(MockLmsCreator)
			creator.On("GetAdapter", "ISIL:REC1").Return(adapter, nil)
			svc := CreatePatronRequestActionService(repo, new(IllRepoMock), bus, new(MockIso18626Handler), creator, new(EmailSenderMock), nil, nil)
			err = svc.RunAutoActionsOnStateEntry(appCtx, pr, nil, "test-user")
			expectedStatus := events.EventStatusProblem
			expectedCalls := int32(2)
			if transportFailure {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret-password")
				expectedStatus, expectedCalls = events.EventStatusError, 1
				require.Equal(t, BorrowerStateNew, repo.savedPr.State)
			} else {
				require.NoError(t, err)
				require.Equal(t, BorrowerStateInvalidPatron, repo.savedPr.State)
				require.True(t, repo.savedPr.NeedsAttention)
			}
			require.Equal(t, expectedCalls, calls.Load())
			require.Len(t, bus.processedTaskEvents, 1)
			result := bus.processedTaskEvents[0]
			require.Equal(t, expectedStatus, result.EventStatus)
			if !transportFailure {
				require.Equal(t, ActionOutcomeReview, result.ResultData.ActionResult.Outcome)
			}
			require.Len(t, bus.createdNoticeStatus, int(expectedCalls))
			for _, status := range bus.createdNoticeStatus {
				require.Equal(t, expectedStatus, status)
			}
		})
	}
}

func TestNcipV1ReservationAgencySurvivesPersistenceAndCompensation(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		failCommit, fallback bool
	}{{"persisted", false, false}, {"compensation", true, false}, {"fallback persisted", false, true}, {"fallback compensation", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			failCommit := tc.failCommit
			initiatingID := "ncip1-request:42"
			returnedID := ncip.UniqueRequestId{XMLName: xml.Name{Local: "UniqueRequestId"}, UniqueAgencyId: ncip.UniqueAgencyId{XMLName: xml.Name{Local: "UniqueAgencyId"}, Scheme: "urn:lms:agency:custom", Value: "other-lms"}, RequestIdentifierValue: " lms-request:&/é "}
			reservation := ncipV1ResponseFixture(t, "RequestItem")
			reservation.RequestItemResponse.UniqueRequestId = &returnedID
			returnedItem := ncip.UniqueItemId{XMLName: xml.Name{Local: "UniqueItemId"}, UniqueAgencyId: returnedID.UniqueAgencyId, ItemIdentifierValue: " internal-item:&/é "}
			reservation.RequestItemResponse.UniqueItemId = &returnedItem
			if tc.fallback {
				reservation.RequestItemResponse.UniqueRequestId = nil
			}
			reservation.RequestItemResponse.ItemOptionalFields = &ncip.ItemOptionalFields{ItemDescription: &ncip.ItemDescription{CopyNumber: "barcode"}}
			cancel := ncipV1ResponseFixture(t, "CancelRequestItem")
			checkout := ncipV1ResponseFixture(t, "CheckOutItem")
			checkin := ncipV1ResponseFixture(t, "CheckInItem")
			var requests, cancellations, checkouts, checkins atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var message ncip.NCIPMessage
				require.NoError(t, xml.NewDecoder(r.Body).Decode(&message))
				var response ncip.NCIPMessage
				switch {
				case message.RequestItem != nil:
					requests.Add(1)
					require.Equal(t, "broker", message.RequestItem.UniqueRequestId.UniqueAgencyId.Value)
					require.Equal(t, initiatingID, message.RequestItem.UniqueRequestId.RequestIdentifierValue)
					if tc.fallback {
						returnedID = *message.RequestItem.UniqueRequestId
					}
					response = reservation
				case message.CancelRequestItem != nil:
					cancellations.Add(1)
					require.Equal(t, returnedID, *message.CancelRequestItem.UniqueRequestId)
					response = cancel
				case message.CheckOutItem != nil:
					checkouts.Add(1)
					require.Equal(t, returnedID, *message.CheckOutItem.UniqueRequestId)
					require.Equal(t, returnedItem, message.CheckOutItem.UniqueItemId)
					response = checkout
				case message.CheckInItem != nil:
					checkins.Add(1)
					require.Equal(t, returnedItem, message.CheckInItem.UniqueItemId)
					response = checkin
				default:
					t.Error("unexpected NCIP service")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/xml")
				require.NoError(t, xml.NewEncoder(w).Encode(response))
			}))
			defer server.Close()
			config := dirapi.LmsConfig{Address: server.URL, FromAgency: "broker"}
			adapter, err := lms.CreateLmsAdapterNcip1(config)
			require.NoError(t, err)
			pr := testLoan()
			pr.RequesterSymbol = getDbText("ISIL:REC1")
			pr.IllRequest.Header.RequestingAgencyRequestId = initiatingID
			pr.IllRequest.BibliographicInfo.SupplierUniqueRecordId = "bib"
			repo := &loanTxRepo{MockPrRepo: &MockPrRepo{savedPr: pr}}
			if failCommit {
				repo.failStep = "commit"
			}
			directory := new(IllRepoMock)
			directory.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything).Return([]ill_db.Peer{{CustomData: dirapi.Entry{}}}, "", nil)
			svc := CreatePatronRequestActionService(repo, directory, new(MockEventBus), new(MockIso18626Handler), nil, nil, nil, nil)
			result := svc.requestItemLenderRequest(appCtx, pr, adapter, pr.IllRequest)
			if failCommit {
				require.Equal(t, events.EventStatusError, result.status)
				require.Empty(t, repo.savedItems)
			} else {
				require.Equal(t, events.EventStatusSuccess, result.status)
				require.Len(t, repo.savedItems, 1)
				require.True(t, repo.savedItems[0].LmsRequestID.Valid)
				// A fresh adapter must reconstruct the wire identifier using only persisted data.
				adapter, err = lms.CreateLmsAdapterNcip1(config)
				require.NoError(t, err)
				require.True(t, repo.savedItems[0].LmsItemID.Valid)
				require.Equal(t, "barcode", repo.savedItems[0].Barcode)
				result = svc.shipLenderRequest(appCtx, "ship", pr, adapter, pr.IllRequest, actionParams{})
				require.Equal(t, events.EventStatusSuccess, result.status, "%+v", result.result)
				result = svc.markReceivedLenderRequest(appCtx, "receive", result.pr, adapter)
				require.Equal(t, events.EventStatusSuccess, result.status, "%+v", result.result)
				require.Equal(t, pr_db.LmsStatusCheckedIn, repo.savedItems[0].LmsStatus)
				require.Equal(t, "barcode", repo.savedItems[0].Barcode)
				require.Equal(t, int32(1), checkins.Load())
				require.NoError(t, svc.cancelLenderRequestItems(appCtx, pr, adapter))
				require.Equal(t, int32(1), checkouts.Load())
			}
			require.Equal(t, int32(1), requests.Load())
			require.Equal(t, int32(1), cancellations.Load())
		})
	}
}
