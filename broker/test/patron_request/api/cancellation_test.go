package prapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequesterCancellationReasonPersistsThroughSupplierAcceptance(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	requesterSymbol := "ISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()
	apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink, dirapi.Entry{}, requesterSymbol)
	apptest.CreatePeerWithModeAndVendor(t, illRepo, supplierSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink, dirapi.Entry{}, supplierSymbol)
	patron := "p1"
	body, err := json.Marshal(proapi.CreatePatronRequest{
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "return-" + supplierSymbol + "::CANCEL-REASON", Title: "Requester cancellation reason regression"},
			ServiceInfo:       &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
		},
	})
	require.NoError(t, err)
	response := httpRequest(t, http.MethodPost, basePath, body, http.StatusCreated)
	var requester proapi.PatronRequest
	require.NoError(t, json.Unmarshal(response, &requester))
	requesterPath := basePath + "/" + requester.Id
	requesterParams := "?side=borrowing&symbol=" + requesterSymbol
	require.True(t, test.WaitForPredicateToBeTrue(func() bool {
		actions := httpRequest(t, http.MethodGet, requesterPath+"/actions"+requesterParams, nil, http.StatusOK)
		return strings.Contains(string(actions), `"name":"cancel-request"`)
	}), "requester cancellation action should become available")
	var supplier pr_db.PatronRequest
	require.True(t, test.WaitForPredicateToBeTrue(func() bool {
		supplier, err = prRepo.GetLendingRequestBySupplierSymbolAndRequesterReqId(ctx, supplierSymbol, requester.Id)
		return err == nil
	}), "supplier patron request should be created")

	supplierID := supplier.ID

	invokeAction := func(path, action string, params map[string]any) {
		t.Helper()
		body, err := json.Marshal(proapi.ExecuteAction{Action: action, ActionParams: &params})
		require.NoError(t, err)
		response := httpRequest(t, http.MethodPost, path, body, http.StatusOK)
		var result proapi.ActionResult
		require.NoError(t, json.Unmarshal(response, &result))
		require.Equal(t, string(events.EventStatusSuccess), result.Result)
	}
	invokeAction(requesterPath+"/action"+requesterParams, string(prservice.BorrowerActionCancelRequest), map[string]any{
		"note": "Please cancel. #CancellationReason: InvalidUser#Ref: 123#",
	})
	borrowing, err := prRepo.GetPatronRequestById(ctx, requester.Id)
	require.NoError(t, err)
	assert.Equal(t, prservice.SideBorrowing, borrowing.Side)
	assert.Equal(t, prservice.BorrowerStateCancelPending, borrowing.State)
	assert.True(t, borrowing.CancellationReason.Valid)
	assert.Equal(t, "InvalidUser", borrowing.CancellationReason.String)

	require.True(t, test.WaitForPredicateToBeTrue(func() bool {
		supplier, err = prRepo.GetPatronRequestById(ctx, supplierID)
		return err == nil && supplier.State == prservice.LenderStateCancelRequested
	}), "supplier should receive cancellation")
	assert.Equal(t, "InvalidUser", supplier.CancellationReason.String)
	notes, _, err := prRepo.GetNotificationsByPrId(ctx, pr_db.GetNotificationsByPrIdParams{PrID: supplierID, Kind: string(pr_db.NotificationKindNote), Limit: 100})
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.Equal(t, "Please cancel. Ref: 123#", notes[0].Note.String)

	// accept-cancel generates no note: the borrowing request must retain its reason
	// without relying on the supplier to echo the microformat.
	invokeAction(basePath+"/"+supplierID+"/action?side=lending&symbol="+supplierSymbol, string(prservice.LenderActionAcceptCancel), map[string]any{})
	require.True(t, test.WaitForPredicateToBeTrue(func() bool {
		borrowing, err = prRepo.GetPatronRequestById(ctx, requester.Id)
		return err == nil && borrowing.State == prservice.BorrowerStateCancelled
	}), "borrowing request should reach CANCELLED")
	assert.True(t, borrowing.CancellationReason.Valid)
	assert.Equal(t, "InvalidUser", borrowing.CancellationReason.String)
}
