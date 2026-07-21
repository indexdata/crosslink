package prapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/oapi"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/ill_db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var basePath = "/patron_requests"
var illRepo ill_db.IllRepo
var prRepo pr_db.PrRepo
var ncipMockUrl string

func TestMain(m *testing.M) {
	app.TENANT_TO_SYMBOL = ""
	ctx := context.Background()
	app.DB_PROVISION = true
	app.DB_EXPLAIN_ANALYZE = false

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	mockPort := utils.Must(test.GetFreePort())
	localAddress := "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
	test.Expect(os.Setenv("PEER_URL", localAddress), "failed to set peer URL")

	adapter.MOCK_PEER_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"
	ncipMockUrl = "http://localhost:" + strconv.Itoa(mockPort) + "/ncip"

	apptest.StartMockApp(mockPort)

	ctx, cancel := context.WithCancel(context.Background())
	_, illRepo, _, prRepo = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(test.TerminatePGContainer(ctx, pgContainer), "failed to stop db container")
	os.Exit(code)
}

func TestCrud(t *testing.T) {
	requesterSymbol := "localISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()

	lmsConfig := &dirapi.LmsConfig{
		FromAgency: "from-agency",
		Address:    ncipMockUrl,
	}
	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink,
		dirapi.Entry{
			LmsConfig: lmsConfig,
		}, requesterSymbol)
	assert.NotNil(t, reqPeer)
	supPeer := apptest.CreatePeer(t, illRepo, supplierSymbol, adapter.MOCK_PEER_URL)
	assert.NotNil(t, supPeer)

	// POST
	patron := "p1"
	request := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "WILLSUPPLY_LOANED",
			Title:                  "Typed request round trip",
			Author:                 "John Wick",
		},
		ServiceInfo: &iso18626.ServiceInfo{
			ServiceLevel: &iso18626.TypeSchemeValuePair{
				Text: "Copy",
			},
			ServiceType: iso18626.TypeServiceTypeCopy,
			NeedBeforeDate: &utils.XSDDateTime{
				Time: time.Now().Add(24 * time.Hour),
			},
		},
		PatronInfo: &iso18626.PatronInfo{
			GivenName: "John",
			Surname:   "Wick",
		},
	}
	id := "REQ-" + strings.ToUpper(uuid.NewString())
	newPr := proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      request,
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	hres, respBytes := httpRequest2(t, "POST", basePath, newPrBytes, 201)
	// Check Location header
	location := hres.Header.Get("Location")
	assert.NotEmpty(t, location, "Location header should be set")
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+id, location)

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.True(t, foundPr.State != "")
	assert.Equal(t, "returnables", foundPr.StateModel)
	assert.Equal(t, string(prservice.SideBorrowing), foundPr.Side)
	if !assert.NotNil(t, foundPr.RequesterSymbol) {
		t.FailNow()
	}
	assert.Equal(t, *newPr.RequesterSymbol, *foundPr.RequesterSymbol)
	assert.Nil(t, foundPr.SupplierSymbol)
	if !assert.NotNil(t, foundPr.Patron) {
		t.FailNow()
	}
	assert.Equal(t, *newPr.Patron, *foundPr.Patron)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "WILLSUPPLY_LOANED", r.BibliographicInfo.SupplierUniqueRecordId)
		assert.Equal(t, "Typed request round trip", r.BibliographicInfo.Title)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
		assert.False(t, r.Header.Timestamp.IsZero())
	})
	if !assert.NotNil(t, foundPr.LastAction) {
		t.FailNow()
	}
	assert.Equal(t, "send-request", *foundPr.LastAction)
	if !assert.NotNil(t, foundPr.LastActionOutcome) {
		t.FailNow()
	}
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)
	assert.NotNil(t, foundPr.NotificationsLink)
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+*newPr.Id+"/notifications?symbol="+url.QueryEscape(*newPr.RequesterSymbol), *foundPr.NotificationsLink)
	assert.NotNil(t, foundPr.ItemsLink)
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+*newPr.Id+"/items?symbol="+url.QueryEscape(*newPr.RequesterSymbol), *foundPr.ItemsLink)
	assert.NotNil(t, foundPr.AvailableActionsLink)
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+*newPr.Id+"/actions?symbol="+url.QueryEscape(*newPr.RequesterSymbol), *foundPr.AvailableActionsLink)
	assert.NotNil(t, foundPr.EventsLink)
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+*newPr.Id+"/events?symbol="+url.QueryEscape(*newPr.RequesterSymbol), *foundPr.EventsLink)
	assert.NotNil(t, foundPr.IllTransactionLink)
	assert.Equal(t, getLocalhostWithPort()+"/ill_transactions?requester_req_id="+url.QueryEscape(*newPr.Id), *foundPr.IllTransactionLink)

	assert.Equal(t, false, foundPr.NeedsAttention)

	respBytes = httpRequest(t, "POST", basePath, newPrBytes, 400)
	assert.Contains(t, string(respBytes), "a patron request with this ID already exists")

	// GET list
	queryParams := "?side=borrowing&symbol=" + *foundPr.RequesterSymbol
	respBytes = httpRequest(t, "GET", basePath+queryParams, []byte{}, 200)
	var foundPrs proapi.PatronRequests
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, *newPr.Id, foundPrs.Items[0].Id)
	assert.Nil(t, foundPrs.About.LastLink)
	assert.NotNil(t, foundPrs.Items[0].NotificationsLink)
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+*newPr.Id+"/notifications?symbol="+url.QueryEscape(*newPr.RequesterSymbol), *foundPrs.Items[0].NotificationsLink)
	assert.NotNil(t, foundPrs.Items[0].ItemsLink)
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+*newPr.Id+"/items?symbol="+url.QueryEscape(*newPr.RequesterSymbol), *foundPrs.Items[0].ItemsLink)
	assert.NotNil(t, foundPrs.Items[0].AvailableActionsLink)
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+*newPr.Id+"/actions?symbol="+url.QueryEscape(*newPr.RequesterSymbol), *foundPrs.Items[0].AvailableActionsLink)
	assert.NotNil(t, foundPrs.Items[0].EventsLink)
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+*newPr.Id+"/events?symbol="+url.QueryEscape(*newPr.RequesterSymbol), *foundPrs.Items[0].EventsLink)
	assert.NotNil(t, foundPrs.Items[0].IllTransactionLink)
	assert.Equal(t, getLocalhostWithPort()+"/ill_transactions?requester_req_id="+url.QueryEscape(*newPr.Id), *foundPrs.Items[0].IllTransactionLink)
	assertPatronRequestIllRequest(t, foundPrs.Items[0].IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "WILLSUPPLY_LOANED", r.BibliographicInfo.SupplierUniqueRecordId)
		assert.Equal(t, "Typed request round trip", r.BibliographicInfo.Title)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
	})

	// GET list symbol/requester_req_id params are translated to exact CQL fields.
	respBytes = httpRequest(t, "GET", basePath+"?side=borrowing&symbol="+url.QueryEscape(strings.ToLower(*foundPr.RequesterSymbol)), []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, int64(0), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 0)

	respBytes = httpRequest(t, "GET", basePath+"?requester_req_id="+url.QueryEscape(*newPr.Id), []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 1)
	assert.Equal(t, *newPr.Id, foundPrs.Items[0].Id)

	respBytes = httpRequest(t, "GET", basePath+"?requester_req_id="+url.QueryEscape(strings.ToLower(*newPr.Id)), []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, int64(0), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 0)

	// GET list with offset in
	respBytes = httpRequest(t, "GET", basePath+queryParams+"&offset=100000", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 0)

	// Poll until the PR is in SHIPPED state and the basic CQL filters match.
	// The full set of CQL filters (has_notification, needs_attention, etc.) cannot be used here
	// because the mock's Loaned SupplyingAgencyMessage arrives asynchronously and permanently
	// flips has_notification to true, making those filters unmatchable.
	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", basePath+queryParams+"&cql=state%3DSHIPPED%20and%20"+
			"side%3Dborrowing%20and%20requester_symbol%3D"+*foundPr.RequesterSymbol+
			"%20and%20requester_req_id%3D"+*foundPr.RequesterRequestId+"%20and%20has_cost%3Dfalse%20and%20"+
			"service_type%3DCopy%20and%20service_level%3DCopy%20and%20created_at%3E2026-03-16%20and%20needed_at%3E2026-03-16"+
			"%20and%20title%3D%22Typed%20request%20round%20trip%22%20and%20patron%3Dp1%20and%20cql.serverChoice%20all%20round%20and%20"+
			"terminal_state%3Dfalse%20and%20title%20%3D%20trip%20and%20author%20%3D%20john%20and%20updated_at%3E2026-03-16%20and%20"+
			"given_name%20%3D%20john%20and%20surname%20%3D%20wick%20sortby%20created_at%2Fsort.descending", []byte{}, 200)
		err = json.Unmarshal(respBytes, &foundPrs)
		return err != nil || len(foundPrs.Items) > 0
	}), "timed out waiting for patron request to reach SHIPPED and match the basic CQL filters")
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Len(t, foundPrs.Items, 1)

	// GET by id with symbol and side
	thisPrPath := basePath + "/" + *newPr.Id

	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
		err = json.Unmarshal(respBytes, &foundPr)
		if err != nil {
			return true
		}
		return foundPr.LastAction != nil && *foundPr.LastAction == "send-notification"
	}), "timed out waiting for patron request to reach send-notification action")
	assert.NoError(t, err, "failed to unmarshal patron request")
	if assert.NotNil(t, foundPr.LastAction) {
		assert.Equal(t, "send-notification", *foundPr.LastAction)
	}
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "Typed request round trip", r.BibliographicInfo.Title)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
	})
	if assert.NotNil(t, foundPr.LastActionOutcome) {
		assert.Equal(t, "success", *foundPr.LastActionOutcome)
	}
	if assert.NotNil(t, foundPr.LastActionResult) {
		assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)
	}

	// GET by id with symbol
	respBytes = httpRequest(t, "GET", thisPrPath+"?symbol="+*foundPr.RequesterSymbol, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "Typed request round trip", r.BibliographicInfo.Title)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
	})

	// GET items (initially empty): should return object with empty items list, not null
	respBytes = httpRequest(t, "GET", thisPrPath+"/items"+queryParams, []byte{}, 200)
	var initialPrItems proapi.PrItems
	err = json.Unmarshal(respBytes, &initialPrItems)
	assert.NoError(t, err, "failed to unmarshal initial patron request items")
	assert.Equal(t, int64(0), initialPrItems.About.Count)
	assert.Equal(t, []proapi.PrItem{}, initialPrItems.Items)

	var action proapi.ExecuteAction
	var pResult proapi.ActionResult

	// Wait till requester response processed
	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
		var allowedActions proapi.AllowedActions
		err = json.Unmarshal(respBytes, &allowedActions)
		if err != nil {
			return false
		}
		for _, a := range allowedActions.Actions {
			if a.Name == string(prservice.BorrowerActionReceive) && a.Available {
				return true
			}
		}
		return false
	}), "timed out waiting for BorrowerActionReceive to become available")

	// POST blocking action
	// Retry while the send-request invoke-action task is still marked in-progress;
	// the state transition (LOANED) can become visible before that task is marked complete.
	action = proapi.ExecuteAction{
		Action: "receive",
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 200)
		err = json.Unmarshal(respBytes, &pResult)
		if err != nil {
			return false
		}
		return pResult.Message == nil || *pResult.Message != "another invoke-action task in progress"
	}), "timed out waiting for receive to run without task conflict")
	// used to succeed, but the illmock currently does not include items as part of the Loaned message, which causes the action to fail.
	// We should either update the mock to include items or change the test to not use blocking action.
	assert.Equal(t, "ERROR", pResult.Result)
	if assert.NotNil(t, pResult.Message) {
		assert.Equal(t, "receiveBorrowingRequest failed to get items by PR ID", *pResult.Message)
	}
	assert.Equal(t, "failure", pResult.Outcome)

	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	if assert.NotNil(t, foundPr.LastAction) {
		assert.Equal(t, "receive", *foundPr.LastAction)
	}
	if assert.NotNil(t, foundPr.LastActionOutcome) {
		assert.Equal(t, "failure", *foundPr.LastActionOutcome)
	}
	if assert.NotNil(t, foundPr.LastActionResult) {
		assert.Equal(t, "ERROR", *foundPr.LastActionResult)
	}

	// TODO Do we really want to delete from DB or just add DELETED status ?
	//// DELETE patron request
	//httpRequest(t, "DELETE", thisPrPath, []byte{}, 204)
	//
	//// GET patron request which is deleted
	//httpRequest(t, "DELETE", thisPrPath, []byte{}, 404)
}

func assertPatronRequestIllRequest(t *testing.T, payload iso18626.Request, assertFn func(iso18626.Request)) {
	t.Helper()
	assertFn(payload)
}

func TestNeedsReviewAndUpdate(t *testing.T) {
	requesterSymbol := "ISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()

	lmsConfig := &dirapi.LmsConfig{
		FromAgency: "from-agency",
		Address:    ncipMockUrl,
	}
	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink,
		dirapi.Entry{LmsConfig: lmsConfig}, requesterSymbol)
	assert.NotNil(t, reqPeer)
	supPeer := apptest.CreatePeer(t, illRepo, supplierSymbol, adapter.MOCK_PEER_URL)
	assert.NotNil(t, supPeer)

	// POST without SupplierUniqueRecordId: validate returns 'review' outcome → NEEDS_REVIEW
	patron := "p1"
	request := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			Title: "Needs review title",
		},
		ServiceInfo: &iso18626.ServiceInfo{
			ServiceType: iso18626.TypeServiceTypeCopy,
		},
	}
	newPr := proapi.CreatePatronRequest{
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      request,
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err)

	respBytes := httpRequest(t, "POST", basePath, newPrBytes, 201)
	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err)
	assert.NotEmpty(t, foundPr.Id)

	prPath := basePath + "/" + foundPr.Id
	queryParams := "?side=borrowing&symbol=" + requesterSymbol

	// Validate auto-action runs synchronously; state should already be NEEDS_REVIEW.
	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", prPath+queryParams, []byte{}, 200)
		err = json.Unmarshal(respBytes, &foundPr)
		return err == nil && foundPr.State == string(prservice.BorrowerStateNeedsReview)
	}), "timed out waiting for NEEDS_REVIEW state")
	assert.Equal(t, string(prservice.BorrowerStateNeedsReview), foundPr.State)
	if assert.NotNil(t, foundPr.LastAction) {
		assert.Equal(t, string(prservice.BorrowerActionValidate), *foundPr.LastAction)
	}
	if assert.NotNil(t, foundPr.LastActionOutcome) {
		assert.Equal(t, prservice.ActionOutcomeReview, *foundPr.LastActionOutcome)
	}
	assert.True(t, foundPr.NeedsAttention)

	// PUT without SupplierUniqueRecordId: state must remain NEEDS_REVIEW
	prId := foundPr.Id
	updateNoId := proapi.CreatePatronRequest{
		Id:              &prId,
		RequesterSymbol: &requesterSymbol,
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{
				Title: "Updated title, still no item ID",
			},
			ServiceInfo: &iso18626.ServiceInfo{
				ServiceType: iso18626.TypeServiceTypeCopy,
			},
		},
	}
	updateNoIdBytes, err := json.Marshal(updateNoId)
	assert.NoError(t, err)
	respBytes = httpRequest(t, "PUT", prPath, updateNoIdBytes, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err)
	assert.Equal(t, string(prservice.BorrowerStateNeedsReview), foundPr.State)
	assert.Equal(t, "returnables", foundPr.StateModel)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "Updated title, still no item ID", r.BibliographicInfo.Title)
		assert.Empty(t, r.BibliographicInfo.SupplierUniqueRecordId)
	})

	// PUT with SupplierUniqueRecordId: PUT only persists data, state stays NEEDS_REVIEW
	updateWithId := proapi.CreatePatronRequest{
		Id:              &prId,
		RequesterSymbol: &requesterSymbol,
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{
				Title:                  "Updated title with item ID",
				SupplierUniqueRecordId: "WILLSUPPLY_LOANED",
			},
			ServiceInfo: &iso18626.ServiceInfo{
				ServiceType: iso18626.TypeServiceTypeCopy,
			},
		},
	}
	updateWithIdBytes, err := json.Marshal(updateWithId)
	assert.NoError(t, err)
	respBytes = httpRequest(t, "PUT", prPath, updateWithIdBytes, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err)
	assert.Equal(t, string(prservice.BorrowerStateNeedsReview), foundPr.State)
	assert.Equal(t, "returnables", foundPr.StateModel)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "WILLSUPPLY_LOANED", r.BibliographicInfo.SupplierUniqueRecordId)
	})

	// Manually invoke send-request: transitions out of NEEDS_REVIEW
	sendAction := proapi.ExecuteAction{Action: string(prservice.BorrowerActionSendRequest)}
	sendActionBytes, err := json.Marshal(sendAction)
	assert.NoError(t, err)
	respBytes = httpRequest(t, "POST", prPath+"/action"+queryParams, sendActionBytes, 200)
	var pResult proapi.ActionResult
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err)
	assert.Equal(t, "SUCCESS", pResult.Result)

	// Wait for state to advance beyond NEEDS_REVIEW (mock responds asynchronously)
	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", prPath+queryParams, []byte{}, 200)
		err = json.Unmarshal(respBytes, &foundPr)
		return err == nil && foundPr.State != string(prservice.BorrowerStateNeedsReview)
	}), "timed out waiting for state to advance past NEEDS_REVIEW after send-request")
	assert.NotEqual(t, string(prservice.BorrowerStateNeedsReview), foundPr.State)
}

func TestActionsToCompleteState(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	requesterSymbol := "ISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()

	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink, dirapi.Entry{}, requesterSymbol)
	assert.NotNil(t, reqPeer)

	lmsConfig := &dirapi.LmsConfig{
		FromAgency: "from-agency",
		Address:    ncipMockUrl,
	}
	supPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, supplierSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink,
		dirapi.Entry{
			LmsConfig: lmsConfig,
		}, supplierSymbol)
	assert.NotNil(t, supPeer)

	// POST
	patron := "p1"
	request := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "return-" + supplierSymbol + "::WILLSUPPLY_LOANED",
		},
		ServiceInfo: &iso18626.ServiceInfo{
			ServiceType: iso18626.TypeServiceTypeLoan,
		},
	}
	newPr := proapi.CreatePatronRequest{
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      request,
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	respBytes := httpRequest(t, "POST", basePath, newPrBytes, 201)

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, strings.ToUpper(strings.Split(requesterSymbol, ":")[1]+"-1"), foundPr.Id)
	assert.Equal(t, "returnables", foundPr.StateModel)
	requesterPrPath := basePath + "/" + foundPr.Id
	queryParams := "?side=borrowing&symbol=" + *foundPr.RequesterSymbol

	// Wait till action available
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.BorrowerActionSendRequest)+"\"")
	})

	// Find supplier patron request
	test.WaitForPredicateToBeTrue(func() bool {
		supPr, _ := prRepo.GetLendingRequestBySupplierSymbolAndRequesterReqId(appCtx, supplierSymbol, foundPr.Id)
		return supPr.ID != ""
	})
	supPr, err := prRepo.GetLendingRequestBySupplierSymbolAndRequesterReqId(appCtx, supplierSymbol, foundPr.Id)
	assert.NoError(t, err)
	assert.NotNil(t, supPr.ID)

	// Wait for action Ship
	supplierPrPath := basePath + "/" + supPr.ID
	supQueryParams := "?side=lending&symbol=" + supplierSymbol
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions"+supQueryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.LenderActionShip)+"\"")
	})

	// Send notification
	notification := proapi.CreatePrNotification{
		Note: "Will ship",
	}
	notificationBytes, err := json.Marshal(notification)
	assert.NoError(t, err, "failed to marshal patron request notification")
	httpRequest(t, "POST", supplierPrPath+"/notifications"+supQueryParams, notificationBytes, 201)

	// Check notification supplier side
	var notifications proapi.PrNotifications
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/notifications"+supQueryParams, []byte{}, 200)
		err = json.Unmarshal(respBytes, &notifications)
		assert.NoError(t, err, "failed to unmarshal patron request notifications")
		return notifications.About.Count > 0
	})
	assert.Equal(t, "SENT", *notifications.Items[0].Receipt)
	assert.Equal(t, "Will ship", *notifications.Items[0].Note)

	// Check notification requester side
	forwardedWillShipNote := "Will ship"
	findNotificationByNote := func(list []proapi.PrNotification, note string) *proapi.PrNotification {
		for i := range list {
			if list[i].Note != nil && *list[i].Note == note {
				return &list[i]
			}
		}
		return nil
	}

	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/notifications"+queryParams, []byte{}, 200)
		err = json.Unmarshal(respBytes, &notifications)
		assert.NoError(t, err, "failed to unmarshal patron request notifications")
		return findNotificationByNote(notifications.Items, forwardedWillShipNote) != nil
	})
	willShipNotification := findNotificationByNote(notifications.Items, forwardedWillShipNote)
	assert.NotNil(t, willShipNotification)

	// Set seen notification
	receipt := proapi.UpdateNotificationReceipt{
		Receipt: "SEEN",
	}
	receiptBytes, err := json.Marshal(receipt)
	assert.NoError(t, err, "failed to marshal patron request notification")
	httpRequest(t, "PUT", requesterPrPath+"/notifications/"+willShipNotification.Id+"/receipt"+queryParams, receiptBytes, 204)

	// Check notification requester side
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/notifications"+queryParams, []byte{}, 200)
		err = json.Unmarshal(respBytes, &notifications)
		assert.NoError(t, err, "failed to unmarshal patron request notifications")
		found := findNotificationByNote(notifications.Items, forwardedWillShipNote)
		return found != nil && found.Receipt != nil && *found.Receipt == "SEEN" && found.AcknowledgedAt != nil
	})
	willShipNotification = findNotificationByNote(notifications.Items, forwardedWillShipNote)
	if assert.NotNil(t, willShipNotification) {
		assert.Equal(t, "SEEN", *willShipNotification.Receipt)
		assert.NotNil(t, willShipNotification.AcknowledgedAt)
	}

	// Ship
	action := proapi.ExecuteAction{
		Action: string(prservice.LenderActionShip),
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action"+supQueryParams, actionBytes, 200)
	var pResult proapi.ActionResult
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)
	assert.Nil(t, pResult.Message)

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.BorrowerActionReceive)+"\"")
	})

	// Receive
	action = proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionReceive),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)
	assert.Nil(t, pResult.Message)

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.BorrowerActionCheckOut)+"\"")
	})

	// Check out
	action = proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionCheckOut),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)
	assert.Nil(t, pResult.Message)

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.BorrowerActionCheckIn)+"\"")
	})

	// Check in
	action = proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionCheckIn),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)
	assert.Nil(t, pResult.Message)

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.BorrowerActionShipReturn)+"\"")
	})

	// Ship return
	action = proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionShipReturn),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)
	assert.Nil(t, pResult.Message)

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions"+supQueryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.LenderActionMarkReceived)+"\"")
	})

	// Ship return
	action = proapi.ExecuteAction{
		Action: string(prservice.LenderActionMarkReceived),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action"+supQueryParams, actionBytes, 200)
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)
	assert.Nil(t, pResult.Message)

	// Check requester patron request done
	respBytes = httpRequest(t, "GET", requesterPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, string(prservice.BorrowerStateCompleted), foundPr.State)
	assert.True(t, foundPr.TerminalState)

	// Check requester patron request event count
	respBytes = httpRequest(t, "GET", requesterPrPath+"/events"+queryParams, []byte{}, 200)
	var events oapi.Events
	err = json.Unmarshal(respBytes, &events)
	assert.NoError(t, err, "failed to unmarshal patron request events")
	assert.True(t, len(events.Items) > 5)
	assert.Equal(t, int64(len(events.Items)), events.About.Count)

	// Check requester patron request item count
	respBytes = httpRequest(t, "GET", requesterPrPath+"/items"+queryParams, []byte{}, 200)
	var prItems proapi.PrItems
	err = json.Unmarshal(respBytes, &prItems)
	assert.NoError(t, err, "failed to unmarshal patron request items")
	assert.Equal(t, int64(1), prItems.About.Count)
	assert.Len(t, prItems.Items, 1)

	// Check requester patron request item count
	respBytes = httpRequest(t, "GET", requesterPrPath+"/notifications"+queryParams, []byte{}, 200)
	var prNotifications proapi.PrNotifications
	err = json.Unmarshal(respBytes, &prNotifications)
	assert.NoError(t, err, "failed to unmarshal patron request notifications")
	assert.True(t, prNotifications.About.Count >= 1)
	finalWillShipNotification := findNotificationByNote(prNotifications.Items, forwardedWillShipNote)
	if assert.NotNil(t, finalWillShipNotification) {
		assert.NotNil(t, finalWillShipNotification.Receipt)
		assert.Equal(t, "SEEN", *finalWillShipNotification.Receipt)
	}

	// Check supplier patron request done
	respBytes = httpRequest(t, "GET", supplierPrPath+supQueryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, supPr.ID, foundPr.Id)
	assert.Equal(t, string(prservice.LenderStateCompleted), foundPr.State)
	assert.True(t, foundPr.TerminalState)

	// Check supplier patron request event count
	respBytes = httpRequest(t, "GET", supplierPrPath+"/events"+supQueryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &events)
	assert.NoError(t, err, "failed to unmarshal patron request events")
	assert.True(t, len(events.Items) > 5)
	assert.Equal(t, int64(len(events.Items)), events.About.Count)
}

func TestRejectRetry(t *testing.T) {
	requesterSymbol := "localISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()

	lmsConfig := &dirapi.LmsConfig{
		FromAgency: "from-agency",
		Address:    ncipMockUrl,
	}
	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink,
		dirapi.Entry{
			LmsConfig: lmsConfig,
		}, requesterSymbol)
	assert.NotNil(t, reqPeer)
	supPeer := apptest.CreatePeer(t, illRepo, supplierSymbol, adapter.MOCK_PEER_URL)
	assert.NotNil(t, supPeer)

	// POST
	patron := "p1"
	request := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "RETRY:NOTFOUNDASCITED",
		},
		ServiceInfo: &iso18626.ServiceInfo{
			ServiceLevel: &iso18626.TypeSchemeValuePair{
				Text: "Copy",
			},
			ServiceType: iso18626.TypeServiceTypeCopy,
		},
	}
	id := "REQ-" + strings.ToUpper(uuid.NewString())
	newPr := proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      request,
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	hres, respBytes := httpRequest2(t, "POST", basePath, newPrBytes, 201)
	// Check Location header
	location := hres.Header.Get("Location")
	assert.NotEmpty(t, location, "Location header should be set")
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+id, location)

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.True(t, foundPr.State != "")
	assert.Equal(t, string(prservice.SideBorrowing), foundPr.Side)
	assert.Equal(t, *newPr.RequesterSymbol, *foundPr.RequesterSymbol)
	assert.Nil(t, foundPr.SupplierSymbol)
	assert.Equal(t, *newPr.Patron, *foundPr.Patron)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "RETRY:NOTFOUNDASCITED", r.BibliographicInfo.SupplierUniqueRecordId)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
		assert.False(t, r.Header.Timestamp.IsZero())
	})
	assert.Equal(t, "send-request", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)
	assert.NotNil(t, foundPr.NotificationsLink)

	// GET list
	queryParams := "?side=borrowing&symbol=" + *foundPr.RequesterSymbol
	respBytes = httpRequest(t, "GET", basePath+queryParams, []byte{}, 200)
	var foundPrs proapi.PatronRequests
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	thisPrPath := basePath + "/" + *newPr.Id

	var pResult proapi.ActionResult

	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.Equal(t, "send-request", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)

	// Wait until we can see possible action reject-retry
	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\"reject-retry\"")
	}), "reject-retry action did not appear in time")
	respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
	assert.Contains(t, string(respBytes), "\"name\":\"reject-retry\"")

	// POST blocking action
	action := proapi.ExecuteAction{
		Action: "reject-retry",
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 200)
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)

	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.Equal(t, "reject-retry", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)

	// reject again - should fail as the request state it terminated
	respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 400)
	assert.Contains(t, string(respBytes), "Action reject-retry is not allowed for patron request")
}

func TestAcceptRetry(t *testing.T) {
	requesterSymbol := "localISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()

	lmsConfig := &dirapi.LmsConfig{
		FromAgency: "from-agency",
		Address:    ncipMockUrl,
	}
	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink,
		dirapi.Entry{
			LmsConfig: lmsConfig,
		}, requesterSymbol)
	assert.NotNil(t, reqPeer)
	supPeer := apptest.CreatePeer(t, illRepo, supplierSymbol, adapter.MOCK_PEER_URL)
	assert.NotNil(t, supPeer)

	// POST
	patron := "p1"
	request := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "RETRY:NOTFOUNDASCITED",
			BibliographicItemId: []iso18626.BibliographicItemId{
				{
					BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{
						Text: "ISBN",
					},
					BibliographicItemIdentifier: "1234567890",
				},
			},
		},
		ServiceInfo: &iso18626.ServiceInfo{
			ServiceLevel: &iso18626.TypeSchemeValuePair{
				Text: "Copy",
			},
			ServiceType: iso18626.TypeServiceTypeCopy,
		},
	}
	id := "REQ-" + strings.ToUpper(uuid.NewString())
	newPr := proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      request,
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	hres, respBytes := httpRequest2(t, "POST", basePath, newPrBytes, 201)
	// Check Location header
	location := hres.Header.Get("Location")
	assert.NotEmpty(t, location, "Location header should be set")
	assert.Equal(t, getLocalhostWithPort()+"/patron_requests/"+id, location)

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, id, foundPr.Id)
	assert.True(t, foundPr.State != "")
	assert.Equal(t, string(prservice.SideBorrowing), foundPr.Side)
	assert.Equal(t, *newPr.RequesterSymbol, *foundPr.RequesterSymbol)
	assert.Nil(t, foundPr.SupplierSymbol)
	assert.Equal(t, *newPr.Patron, *foundPr.Patron)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "RETRY:NOTFOUNDASCITED", r.BibliographicInfo.SupplierUniqueRecordId)
		assert.Equal(t, id, r.Header.RequestingAgencyRequestId)
		assert.False(t, r.Header.Timestamp.IsZero())
	})
	assert.Equal(t, "send-request", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)
	assert.NotNil(t, foundPr.NotificationsLink)

	// GET list
	queryParams := "?side=borrowing&symbol=" + *foundPr.RequesterSymbol
	respBytes = httpRequest(t, "GET", basePath+queryParams, []byte{}, 200)
	var foundPrs proapi.PatronRequests
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	thisPrPath := basePath + "/" + *newPr.Id

	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	foundPr = proapi.PatronRequest{}
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.Equal(t, "send-request", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)

	// Wait until we can see possible action accept-retry
	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\"accept-retry\"")
	}), "accept-retry action did not appear in time")
	respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
	assert.Contains(t, string(respBytes), "\"name\":\"accept-retry\"")

	// POST blocking action
	action := proapi.ExecuteAction{
		Action: "accept-retry",
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 200)
	var pResult proapi.ActionResult
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)

	// check the original request is updated with retry info and new request is created
	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	foundPr = proapi.PatronRequest{}
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.Equal(t, "accept-retry", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)
	assert.NotNil(t, foundPr.NextReqId, "got pr "+string(respBytes))
	assert.Nil(t, foundPr.PrevReqId, "got pr "+string(respBytes))
	assert.Equal(t, "123456789", foundPr.RetryBibInfo.SupplierUniqueRecordId)

	// accept again - should fail as the request state it terminated
	respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 400)
	assert.Contains(t, string(respBytes), "Action accept-retry is not allowed for patron request")

	// check cloned request
	newId := *foundPr.NextReqId
	assert.NotEqual(t, newId, id)

	thisPrPath = basePath + "/" + newId

	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	foundPr = proapi.PatronRequest{}
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, newId, foundPr.Id)
	assert.Equal(t, "send-request", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)
	assert.Equal(t, id, *foundPr.PrevReqId)
	assert.Nil(t, foundPr.NextReqId)
	assert.Nil(t, foundPr.RetryBibInfo)
	assert.Equal(t, "123456789", foundPr.IllRequest.BibliographicInfo.SupplierUniqueRecordId)
}

func TestPostPatronRequestRejectsInvalidIllRequest(t *testing.T) {
	requesterSymbol := "localISIL:REQ" + uuid.NewString()

	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink,
		dirapi.Entry{
			LmsConfig: &dirapi.LmsConfig{
				FromAgency: "from-agency",
				Address:    ncipMockUrl,
			},
		}, requesterSymbol)
	assert.NotNil(t, reqPeer)

	newPr := proapi.CreatePatronRequest{
		RequesterSymbol: &requesterSymbol,
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{
				Title: "Invalid request",
			},
			ServiceInfo: &iso18626.ServiceInfo{
				ServiceType: iso18626.TypeServiceType("Broken"),
			},
		},
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	respBytes := httpRequest(t, "POST", basePath, newPrBytes, 400)
	assert.Contains(t, string(respBytes), "invalid illRequest")
	assert.Contains(t, string(respBytes), "ServiceType")
}

func TestGetReturnableStateModel(t *testing.T) {
	respBytes := httpRequest(t, "GET", "/state_model/models/returnables", []byte{}, 200)
	var retrievedStateModel proapi.StateModel
	err := json.Unmarshal(respBytes, &retrievedStateModel)
	assert.NoError(t, err, "failed to unmarshal state model")
	returnablesStateModel, _ := prservice.LoadStateModelByName("returnables")
	assert.Equal(t, returnablesStateModel.Name, retrievedStateModel.Name)
	assert.Equal(t, returnablesStateModel.Desc, retrievedStateModel.Desc)
	assert.Equal(t, len(returnablesStateModel.States), len(retrievedStateModel.States))
}

func TestGetStateModelCapabilities(t *testing.T) {
	respBytes := httpRequest(t, "GET", "/state_model/capabilities", []byte{}, 200)
	var capabilities proapi.StateModelCapabilities
	err := json.Unmarshal(respBytes, &capabilities)
	assert.NoError(t, err, "failed to unmarshal state model capabilities")
	assert.True(t, slices.Contains(capabilities.RequesterStates, string(prservice.BorrowerStateValidated)))
	assert.True(t, slices.Contains(capabilities.SupplierMessageEvents, string(prservice.SupplierWillSupply)))
	assert.True(t, slices.Contains(capabilities.RequesterMessageEvents, string(prservice.RequesterCancelRequest)))
	assert.True(t, slices.Contains(capabilities.RequesterMessageEvents, string(prservice.RequesterReceived)))
	assert.True(t, slices.Contains(capabilities.SupplierMessageEvents, string(prservice.SupplierCancelRejected)))
}

func TestServerChoice(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	prId := uuid.NewString()
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: prId,
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		Side: prservice.SideBorrowing,
		RequesterSymbol: pgtype.Text{
			String: "ISIL:REQ",
			Valid:  true,
		},
		State:    prservice.BorrowerStateValidated,
		Language: "english",
		RequesterReqID: pgtype.Text{
			String: "REQ-123",
			Valid:  true,
		},
		Patron: pgtype.Text{
			String: "P456",
			Valid:  true,
		},
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{
				Title:  "Do Androids Dream of Electric Sheep?",
				Author: "Ray Bradbury",
			},
			PatronInfo: &iso18626.PatronInfo{
				GivenName: "John",
				Surname:   "Doe",
				PatronId:  "PP-789",
			},
		},
		Items:         []pr_db.PrItem{},
		TerminalState: false,
	})
	assert.NoError(t, err)
	itemId := uuid.NewString()
	_, err = prRepo.SaveItem(appCtx, pr_db.SaveItemParams{
		ID:      itemId,
		PrID:    prId,
		Barcode: "BAR-321",
		CallNumber: pgtype.Text{
			String: "CAL-321",
			Valid:  true,
		},
		ItemID: pgtype.Text{
			String: "ITEM-321",
			Valid:  true,
		},
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
	})
	assert.NoError(t, err)

	respBytes := httpRequest(t, "GET", basePath+"?symbol=ISIL:REQ&side=borrowing&cql=cql.serverChoice%20all%20%22REQ-123%20P456%20Dream%20Ray%20Bradbury%20BAR-321%20CAL-321%20ITEM-321%22", []byte{}, 200)
	var foundPrs proapi.PatronRequests
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, prId, foundPrs.Items[0].Id)

	respBytes = httpRequest(t, "GET", basePath+"?symbol=ISIL:REQ&side=borrowing&cql=cql.serverChoice%20all%20%22REQ-123%20P456%20ddream%20Ray%20Bradbury%20BAR-321%20CAL-321%20ITEM-321%22", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), foundPrs.About.Count)
}

func TestRequesterSupplierNameCQL(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqSymbol := "ISIL:REQ-" + uuid.NewString()
	reqName := strings.Replace(reqSymbol, "ISIL:", "NAME:", 1)
	supSymbol := "ISIL:SUP-" + uuid.NewString()
	supName := strings.Replace(supSymbol, "ISIL:", "NAME:", 1)

	// CreatePeerWithModeAndVendor registers the symbol record (reqSymbol/supSymbol) and sets
	// peer.Name to the provided name, so requester_name / supplier_name resolve via the view JOIN.
	apptest.CreatePeerWithModeAndVendor(t, illRepo, reqSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.ReShare, dirapi.Entry{}, reqName)
	apptest.CreatePeerWithModeAndVendor(t, illRepo, supSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.ReShare, dirapi.Entry{}, supName)

	prId := uuid.NewString()
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID:              prId,
		CreatedAt:       pgtype.Timestamp{Time: time.Now(), Valid: true},
		Side:            prservice.SideBorrowing,
		RequesterSymbol: pgtype.Text{String: reqSymbol, Valid: true},
		SupplierSymbol:  pgtype.Text{String: supSymbol, Valid: true},
		State:           prservice.BorrowerStateValidated,
		Language:        "english",
		IllRequest:      iso18626.Request{},
		Items:           []pr_db.PrItem{},
		TerminalState:   false,
	})
	assert.NoError(t, err)

	var foundPrs proapi.PatronRequests

	// requester_name matches → 1 result
	respBytes := httpRequest(t, "GET", basePath+"?cql=requester_name%3D"+url.QueryEscape(reqName), []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, prId, foundPrs.Items[0].Id)

	// requester_name no match → 0 results
	respBytes = httpRequest(t, "GET", basePath+"?cql=requester_name%3DNoSuchLibrary", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), foundPrs.About.Count)

	// supplier_name matches → 1 result
	respBytes = httpRequest(t, "GET", basePath+"?cql=supplier_name%3D"+url.QueryEscape(supName), []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, prId, foundPrs.Items[0].Id)

	// Use WithLikeOps mask → 1 result
	prefixLen := 10
	if len(supName) < prefixLen {
		prefixLen = len(supName)
	}
	prefix := supName[:prefixLen] + "*"
	respBytes = httpRequest(t, "GET", basePath+"?cql=supplier_name%3D"+url.QueryEscape(prefix), []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, prId, foundPrs.Items[0].Id)

	// Use lowercase → 1 result
	lower := strings.ToLower(supName)
	respBytes = httpRequest(t, "GET", basePath+"?cql=supplier_name%3D"+url.QueryEscape(lower), []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, prId, foundPrs.Items[0].Id)

	// supplier_name no match → 0 results
	respBytes = httpRequest(t, "GET", basePath+"?cql=supplier_name%3DNoSuchLibrary", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), foundPrs.About.Count)

	// get requester_name and supplier_name facets → 1 result, 2 facets
	respBytes = httpRequest(t, "GET", basePath+"?cql=requester_name%3D"+url.QueryEscape(reqName)+
		"&facets=requester_name,supplier_name", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, prId, foundPrs.Items[0].Id)

	assert.NotNil(t, foundPrs.About.Facets)
	assert.Len(t, *foundPrs.About.Facets, 2)
	assert.Equal(t, "requester_name", (*foundPrs.About.Facets)[0].Name)
	assert.Len(t, (*foundPrs.About.Facets)[0].Values, 1)
	assert.Equal(t, reqName, (*foundPrs.About.Facets)[0].Values[0].Value)
	assert.Equal(t, int64(1), (*foundPrs.About.Facets)[0].Values[0].Count)
	assert.Equal(t, "supplier_name", (*foundPrs.About.Facets)[1].Name)
	assert.Len(t, (*foundPrs.About.Facets)[1].Values, 1)
	assert.Equal(t, supName, (*foundPrs.About.Facets)[1].Values[0].Value)
	assert.Equal(t, int64(1), (*foundPrs.About.Facets)[1].Values[0].Count)
}

func httpRequest2(t *testing.T, method string, uriPath string, reqbytes []byte, expectStatus int) (*http.Response, []byte) {
	client := http.DefaultClient
	hreq, err := http.NewRequest(method, getLocalhostWithPort()+uriPath, bytes.NewBuffer(reqbytes))
	assert.NoError(t, err)
	hreq.Header.Set("X-Okapi-Tenant", "testlib")

	if method == "POST" || method == "PUT" {
		hreq.Header.Set("Content-Type", "application/json")
	}
	hres, err := client.Do(hreq)
	assert.NoError(t, err)
	defer hres.Body.Close()
	body, err := io.ReadAll(hres.Body)
	assert.Equal(t, expectStatus, hres.StatusCode, string(body))
	assert.NoError(t, err)
	return hres, body
}

func httpRequest(t *testing.T, method string, uriPath string, reqbytes []byte, expectStatus int) []byte {
	_, respBytes := httpRequest2(t, method, uriPath, reqbytes, expectStatus)
	return respBytes
}

func getLocalhostWithPort() string {
	return "http://localhost:" + strconv.Itoa(app.HTTP_PORT)
}

func TestFacetsOK(t *testing.T) {
	requesterSymbols := []string{"ISIL:REQ" + uuid.NewString(), "ISIL:REQ" + uuid.NewString()}

	for _, requesterSymbol := range requesterSymbols {
		reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink,
			dirapi.Entry{
				LmsConfig: &dirapi.LmsConfig{
					FromAgency: "from-agency",
					Address:    ncipMockUrl,
				},
			}, requesterSymbol)
		assert.NotNil(t, reqPeer)
	}

	for i := 0; i < 10; i++ {
		serviceType := "Copy"
		if i%2 == 0 {
			serviceType = "Loan"
		}
		j := 0
		if i >= 7 {
			j = 1
		}
		// POST
		patron := "p1"
		request := iso18626.Request{
			ServiceInfo: &iso18626.ServiceInfo{
				ServiceType: iso18626.TypeServiceType(serviceType),
			},
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: uuid.NewString(),
				Title:                  "Facets title " + strconv.Itoa(i),
			},
		}
		newPr := proapi.CreatePatronRequest{
			RequesterSymbol: &requesterSymbols[j],
			Patron:          &patron,
			IllRequest:      request,
		}
		newPrBytes, err := json.Marshal(newPr)
		assert.NoError(t, err, "failed to marshal patron request")

		respBytes := httpRequest(t, "POST", basePath, newPrBytes, 201)

		var foundPr proapi.PatronRequest
		err = json.Unmarshal(respBytes, &foundPr)
		assert.NoError(t, err, "failed to unmarshal patron request")
	}

	var foundPrs proapi.PatronRequests
	respBytes := httpRequest(t, "GET", basePath+"?cql=title%3Dfacets%20title&offset=0&limit=1", []byte{}, 200)
	err := json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 1)
	assert.Nil(t, foundPrs.About.Facets)

	httpRequest(t, "GET", basePath+"?cql=title%3Dfacets%20title&offset=0&limit=1&facets=", []byte{}, 400)

	respBytes = httpRequest(t, "GET", basePath+"?facets=requester_symbol&cql=service_type%3DCopy+and+title%3Dfacets%20title&offset=0&limit=0", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(5), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 0)
	assert.NotNil(t, foundPrs.About.Facets)
	assert.Len(t, *foundPrs.About.Facets, 1)
	assert.Equal(t, "requester_symbol", (*foundPrs.About.Facets)[0].Name)
	assert.Len(t, (*foundPrs.About.Facets)[0].Values, 2)
	assert.Equal(t, requesterSymbols[0], (*foundPrs.About.Facets)[0].Values[0].Value)
	assert.Equal(t, int64(3), (*foundPrs.About.Facets)[0].Values[0].Count)
	assert.Equal(t, requesterSymbols[1], (*foundPrs.About.Facets)[0].Values[1].Value)
	assert.Equal(t, int64(2), (*foundPrs.About.Facets)[0].Values[1].Count)

	respBytes = httpRequest(t, "GET", basePath+"?facets=requester_symbol&cql=title%3Dfacets%20title&offset=0&limit=0", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 0)
	assert.NotNil(t, foundPrs.About.Facets)
	assert.Len(t, *foundPrs.About.Facets, 1)
	assert.Equal(t, "requester_symbol", (*foundPrs.About.Facets)[0].Name)
	assert.Len(t, (*foundPrs.About.Facets)[0].Values, 2)
	assert.Equal(t, requesterSymbols[0], (*foundPrs.About.Facets)[0].Values[0].Value)
	assert.Equal(t, int64(7), (*foundPrs.About.Facets)[0].Values[0].Count)
	assert.Equal(t, requesterSymbols[1], (*foundPrs.About.Facets)[0].Values[1].Value)
	assert.Equal(t, int64(3), (*foundPrs.About.Facets)[0].Values[1].Count)

	respBytes = httpRequest(t, "GET", basePath+"?facets=requester_symbol%2Csupplier_symbol&cql=title%3Dfacets%20title&offset=0&limit=0", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 0)
	assert.NotNil(t, foundPrs.About.Facets)
	assert.Len(t, *foundPrs.About.Facets, 2)
	assert.Equal(t, "requester_symbol", (*foundPrs.About.Facets)[0].Name)
	assert.Len(t, (*foundPrs.About.Facets)[0].Values, 2)
	assert.Equal(t, requesterSymbols[0], (*foundPrs.About.Facets)[0].Values[0].Value)
	assert.Equal(t, int64(7), (*foundPrs.About.Facets)[0].Values[0].Count)
	assert.Equal(t, requesterSymbols[1], (*foundPrs.About.Facets)[0].Values[1].Value)
	assert.Equal(t, int64(3), (*foundPrs.About.Facets)[0].Values[1].Count)
	assert.Equal(t, "supplier_symbol", (*foundPrs.About.Facets)[1].Name)
	if len((*foundPrs.About.Facets)[1].Values) == 1 {
		assert.Equal(t, "ISIL:BROKER", (*foundPrs.About.Facets)[1].Values[0].Value) // if sent and received by supplier
	} else {
		assert.Empty(t, (*foundPrs.About.Facets)[1].Values) // if not received by supplier yet
	}

	// supplier_symbol values may be empty until the supplier-side request has been created/processed

	// omit CQL (all records), we might get more results than in earlier tests
	respBytes = httpRequest(t, "GET", basePath+"?facets=requester_symbol%2Csupplier_symbol&offset=0&limit=0", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, foundPrs.About.Count, int64(10))
	assert.Len(t, foundPrs.Items, 0)
	assert.NotNil(t, foundPrs.About.Facets)
	assert.GreaterOrEqual(t, len(*foundPrs.About.Facets), 2)
}

func TestFacetsUnknownField(t *testing.T) {
	respBytes := httpRequest(t, "GET", basePath+"?facets=nosuch", []byte{}, 400)
	assert.Contains(t, string(respBytes), "parameter \\\"facets\\\" in query")
}

func TestFacetsEmptyField(t *testing.T) {
	respBytes := httpRequest(t, "GET", basePath+"?facets=", []byte{}, 400)
	assert.Contains(t, string(respBytes), "parameter \\\"facets\\\" in query")
}

func TestCRUDTemplate(t *testing.T) {
	symbol := "ISIL:TMPL" + uuid.NewString()
	apptest.CreatePeerWithModeAndVendor(t, illRepo, symbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink, dirapi.Entry{}, symbol)

	templatePath := "/templates"
	queryParams := "?symbol=" + url.QueryEscape(symbol)

	// POST – create a template
	audience := proapi.TemplateAudiencePatron
	subject := "Your ILL request {{title}} is ready"
	newTemplate := proapi.CreateTemplate{
		Title:       "Ready notification",
		Purpose:     proapi.Email,
		ContentType: proapi.Text,
		Audience:    &audience,
		Subject:     &subject,
		Labels:      []string{"borrower-loaned"},
		Body:        "Dear {{patronName}}, your item {{title}} has arrived.",
	}
	newTemplateBytes, err := json.Marshal(newTemplate)
	assert.NoError(t, err)

	respBytes := httpRequest(t, "POST", templatePath+queryParams, newTemplateBytes, 201)
	var createdTemplate proapi.Template
	err = json.Unmarshal(respBytes, &createdTemplate)
	assert.NoError(t, err)
	assert.NotEmpty(t, createdTemplate.Id)
	assert.Equal(t, newTemplate.Title, createdTemplate.Title)
	assert.Equal(t, newTemplate.Purpose, createdTemplate.Purpose)
	assert.Equal(t, newTemplate.ContentType, createdTemplate.ContentType)
	assert.Equal(t, audience, *createdTemplate.Audience)
	assert.Equal(t, subject, *createdTemplate.Subject)
	assert.Equal(t, newTemplate.Labels, createdTemplate.Labels)
	assert.Equal(t, newTemplate.Body, createdTemplate.Body)
	assert.False(t, createdTemplate.CreatedAt.IsZero())
	assert.Nil(t, createdTemplate.UpdatedAt)

	templateId := createdTemplate.Id
	thisTemplatePath := templatePath + "/" + templateId

	// GET list – template appears in list for correct symbol
	respBytes = httpRequest(t, "GET", templatePath+queryParams, []byte{}, 200)
	var templates proapi.Templates
	err = json.Unmarshal(respBytes, &templates)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), templates.About.Count)
	assert.Len(t, templates.Items, 1)
	assert.Equal(t, templateId, templates.Items[0].Id)

	// GET list – template is NOT visible for a different symbol
	otherSymbol := "ISIL:OTHER" + uuid.NewString()
	apptest.CreatePeerWithModeAndVendor(t, illRepo, otherSymbol, adapter.MOCK_PEER_URL, app.BROKER_MODE, dirapi.CrossLink, dirapi.Entry{}, otherSymbol)
	respBytes = httpRequest(t, "GET", templatePath+"?symbol="+url.QueryEscape(otherSymbol), []byte{}, 200)
	var otherTemplates proapi.Templates
	err = json.Unmarshal(respBytes, &otherTemplates)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), otherTemplates.About.Count)
	assert.Len(t, otherTemplates.Items, 0)

	// GET by id
	respBytes = httpRequest(t, "GET", thisTemplatePath+queryParams, []byte{}, 200)
	var foundTemplate proapi.Template
	err = json.Unmarshal(respBytes, &foundTemplate)
	assert.NoError(t, err)
	assert.Equal(t, templateId, foundTemplate.Id)
	assert.Equal(t, newTemplate.Title, foundTemplate.Title)

	// GET by id – 404 for wrong owner
	httpRequest(t, "GET", thisTemplatePath+"?symbol="+url.QueryEscape(otherSymbol), []byte{}, 404)

	// PUT – update the template
	updatedAudience := proapi.TemplateAudienceStaff
	updatedSubject := "Staff: ILL item {{title}} ready for {{patronName}}"
	updateTemplate := proapi.UpdateTemplate{
		Title:       "Ready notification – updated",
		ContentType: proapi.Html,
		Audience:    &updatedAudience,
		Subject:     &updatedSubject,
		Labels:      []string{"borrower-loaned", "staff"},
		Body:        "<p>Dear {{patronName}}, your item is ready.</p>",
	}
	updateBytes, err := json.Marshal(updateTemplate)
	assert.NoError(t, err)

	respBytes = httpRequest(t, "PUT", thisTemplatePath+queryParams, updateBytes, 200)
	var updatedTemplate proapi.Template
	err = json.Unmarshal(respBytes, &updatedTemplate)
	assert.NoError(t, err)
	assert.Equal(t, templateId, updatedTemplate.Id)
	assert.Equal(t, updateTemplate.Title, updatedTemplate.Title)
	assert.Equal(t, proapi.Html, updatedTemplate.ContentType)
	assert.Equal(t, updatedAudience, *updatedTemplate.Audience)
	assert.Equal(t, updatedSubject, *updatedTemplate.Subject)
	assert.Equal(t, updateTemplate.Labels, updatedTemplate.Labels)
	assert.Equal(t, updateTemplate.Body, updatedTemplate.Body)
	assert.NotNil(t, updatedTemplate.UpdatedAt)

	// PUT – 404 for wrong owner
	httpRequest(t, "PUT", thisTemplatePath+"?symbol="+url.QueryEscape(otherSymbol), updateBytes, 404)

	// DELETE – 404 for wrong owner
	httpRequest(t, "DELETE", thisTemplatePath+"?symbol="+url.QueryEscape(otherSymbol), []byte{}, 404)

	// DELETE
	httpRequest(t, "DELETE", thisTemplatePath+queryParams, []byte{}, 204)

	// GET by id after delete – 404
	httpRequest(t, "GET", thisTemplatePath+queryParams, []byte{}, 404)

	// GET list after delete – empty
	respBytes = httpRequest(t, "GET", templatePath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &templates)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), templates.About.Count)
	assert.Len(t, templates.Items, 0)
}
