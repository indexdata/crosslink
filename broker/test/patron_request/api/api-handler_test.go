package prapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	"github.com/indexdata/crosslink/directory"
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

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
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

	adapter.MOCK_CLIENT_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"
	ncipMockUrl = "http://localhost:" + strconv.Itoa(mockPort) + "/ncip"

	apptest.StartMockApp(mockPort)

	ctx, cancel := context.WithCancel(context.Background())
	_, illRepo, _, prRepo = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestCrud(t *testing.T) {
	requesterSymbol := "localISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()

	lmsConfig := &directory.LmsConfig{
		FromAgency: "from-agency",
		Address:    ncipMockUrl,
	}
	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_CLIENT_URL, app.BROKER_MODE, directory.CrossLink,
		directory.Entry{
			LmsConfig: lmsConfig,
		})
	assert.NotNil(t, reqPeer)
	supPeer := apptest.CreatePeer(t, illRepo, supplierSymbol, adapter.MOCK_CLIENT_URL)
	assert.NotNil(t, supPeer)

	// POST
	patron := "p1"
	request := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "WILLSUPPLY_LOANED",
			Title:                  "Typed request round trip",
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
	}
	id := uuid.NewString()
	newPr := proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      utils.Must(common.StructToMap(request)),
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
		assert.Equal(t, "WILLSUPPLY_LOANED", r.BibliographicInfo.SupplierUniqueRecordId)
		assert.Equal(t, "Typed request round trip", r.BibliographicInfo.Title)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
		assert.False(t, r.Header.Timestamp.IsZero())
	})
	assert.Equal(t, "validate", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)

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
	assertPatronRequestIllRequest(t, foundPrs.Items[0].IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "WILLSUPPLY_LOANED", r.BibliographicInfo.SupplierUniqueRecordId)
		assert.Equal(t, "Typed request round trip", r.BibliographicInfo.Title)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
	})

	// GET list with offset in
	respBytes = httpRequest(t, "GET", basePath+queryParams+"&offset=100000", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 0)

	// GET list with all query params
	respBytes = httpRequest(t, "GET", basePath+queryParams+"&cql=state%3DVALIDATED%20and%20"+
		"side%3Dborrowing%20and%20requester_symbol%3D"+*foundPr.RequesterSymbol+
		"%20and%20requester_req_id%3D"+*foundPr.RequesterRequestId+"%20and%20needs_attention%3Dfalse%20and%20"+
		"has_notification%3Dfalse%20and%20has_cost%3Dfalse%20and%20has_unread_notification%3Dfalse%20and%20"+
		"service_type%3DCopy%20and%20service_level%3DCopy%20and%20created_at%3E2026-03-16%20and%20needed_at%3E2026-03-16"+
		"%20and%20title%3D%22Typed%20request%20round%20trip%22%20and%20patron%3Dp1%20and%20cql.serverChoice%20all%20round%20and%20terminal_state%3Dfalse", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 1)

	// GET by id with symbol and side
	thisPrPath := basePath + "/" + *newPr.Id
	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "Typed request round trip", r.BibliographicInfo.Title)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
	})
	assert.Equal(t, "validate", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)

	// GET by id with symbol
	respBytes = httpRequest(t, "GET", thisPrPath+"?symbol="+*foundPr.RequesterSymbol, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assertPatronRequestIllRequest(t, foundPr.IllRequest, func(r iso18626.Request) {
		assert.Equal(t, "Typed request round trip", r.BibliographicInfo.Title)
		assert.Equal(t, *newPr.Id, r.Header.RequestingAgencyRequestId)
	})

	// GET actions by PR id
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\"send-request\"")
	})
	respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
	assert.Equal(t, "{\"actions\":[{\"name\":\"send-request\",\"parameters\":[],\"primary\":true}]}\n", string(respBytes))

	// POST execute action
	action := proapi.ExecuteAction{
		Action: "send-request",
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 200)
	var pResult proapi.ActionResult
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)
	assert.Equal(t, "success", pResult.Outcome)
	assert.Equal(t, "VALIDATED", pResult.FromState)
	assert.Equal(t, "SENT", *pResult.ToState)
	assert.Nil(t, pResult.Message)

	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.Equal(t, "send-request", *foundPr.LastAction)
	assert.Equal(t, "success", *foundPr.LastActionOutcome)
	assert.Equal(t, "SUCCESS", *foundPr.LastActionResult)

	// Wait till requester response processed
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\"receive\"")
	})

	// POST blocking action
	action = proapi.ExecuteAction{
		Action: "receive",
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 200)
	// used to succeed, but the illmock currently does not include items as part of the Loaned message, which causes the action to fail.
	// We should either update the mock to include items or change the test to not use blocking action.
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "ERROR", pResult.Result)
	assert.Equal(t, "receiveBorrowingRequest failed to get items by PR ID", *pResult.Message)
	assert.Equal(t, "failure", pResult.Outcome)

	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.Equal(t, "receive", *foundPr.LastAction)
	assert.Equal(t, "failure", *foundPr.LastActionOutcome)
	assert.Equal(t, "ERROR", *foundPr.LastActionResult)

	// TODO Do we really want to delete from DB or just add DELETED status ?
	//// DELETE patron request
	//httpRequest(t, "DELETE", thisPrPath, []byte{}, 204)
	//
	//// GET patron request which is deleted
	//httpRequest(t, "DELETE", thisPrPath, []byte{}, 404)
}

func assertPatronRequestIllRequest(t *testing.T, payload map[string]interface{}, assertFn func(iso18626.Request)) {
	t.Helper()

	data, err := json.Marshal(payload)
	assert.NoError(t, err)

	var request iso18626.Request
	err = json.Unmarshal(data, &request)
	assert.NoError(t, err)

	assertFn(request)
}

func TestActionsToCompleteState(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	requesterSymbol := "ISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()

	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_CLIENT_URL, app.BROKER_MODE, directory.CrossLink, directory.Entry{})
	assert.NotNil(t, reqPeer)

	lmsConfig := &directory.LmsConfig{
		FromAgency: "from-agency",
		Address:    ncipMockUrl,
	}
	supPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, supplierSymbol, adapter.MOCK_CLIENT_URL, app.BROKER_MODE, directory.CrossLink,
		directory.Entry{
			LmsConfig: lmsConfig,
		})
	assert.NotNil(t, supPeer)

	// POST
	patron := "p1"
	request := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "return-" + supplierSymbol + "::WILLSUPPLY_LOANED",
		},
	}
	newPr := proapi.CreatePatronRequest{
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      utils.Must(common.StructToMap(request)),
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	respBytes := httpRequest(t, "POST", basePath, newPrBytes, 201)

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, strings.ToUpper(strings.Split(requesterSymbol, ":")[1]+"-1"), foundPr.Id)
	requesterPrPath := basePath + "/" + foundPr.Id
	queryParams := "?side=borrowing&symbol=" + *foundPr.RequesterSymbol

	// Wait till action available
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.BorrowerActionSendRequest)+"\"")
	})

	action := proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionSendRequest),
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	var pResult proapi.ActionResult
	err = json.Unmarshal(respBytes, &pResult)
	assert.NoError(t, err, "failed to unmarshal patron request action result")
	assert.Equal(t, "SUCCESS", pResult.Result)
	assert.Nil(t, pResult.Message)

	// Find supplier patron request
	test.WaitForPredicateToBeTrue(func() bool {
		supPr, _ := prRepo.GetLendingRequestBySupplierSymbolAndRequesterReqId(appCtx, supplierSymbol, foundPr.Id)
		return supPr.ID != ""
	})
	supPr, err := prRepo.GetLendingRequestBySupplierSymbolAndRequesterReqId(appCtx, supplierSymbol, foundPr.Id)
	assert.NoError(t, err)
	assert.NotNil(t, supPr.ID)

	// Wait for action
	supplierPrPath := basePath + "/" + supPr.ID
	supQueryParams := "?side=lending&symbol=" + supplierSymbol
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions"+supQueryParams, []byte{}, 200)
		return strings.Contains(string(respBytes), "\"name\":\""+string(prservice.LenderActionShip)+"\"")
	})

	// Ship
	action = proapi.ExecuteAction{
		Action: string(prservice.LenderActionShip),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action"+supQueryParams, actionBytes, 200)
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
	var events []oapi.Event
	err = json.Unmarshal(respBytes, &events)
	assert.NoError(t, err, "failed to unmarshal patron request events")
	assert.True(t, len(events) > 5)

	// Check requester patron request item count
	respBytes = httpRequest(t, "GET", requesterPrPath+"/items"+queryParams, []byte{}, 200)
	var prItems []proapi.PrItem
	err = json.Unmarshal(respBytes, &prItems)
	assert.NoError(t, err, "failed to unmarshal patron request items")
	assert.Len(t, prItems, 1)

	// Check requester patron request item count
	respBytes = httpRequest(t, "GET", requesterPrPath+"/notifications"+queryParams, []byte{}, 200)
	var prNotifications []proapi.PrNotification
	err = json.Unmarshal(respBytes, &prNotifications)
	assert.NoError(t, err, "failed to unmarshal patron request notifications")
	assert.True(t, len(prNotifications) >= 4)

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
	assert.True(t, len(events) > 5)
}

func TestPostPatronRequestRejectsInvalidIllRequest(t *testing.T) {
	requesterSymbol := "localISIL:REQ" + uuid.NewString()

	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_CLIENT_URL, app.BROKER_MODE, directory.CrossLink,
		directory.Entry{
			LmsConfig: &directory.LmsConfig{
				FromAgency: "from-agency",
				Address:    ncipMockUrl,
			},
		})
	assert.NotNil(t, reqPeer)

	newPr := proapi.CreatePatronRequest{
		RequesterSymbol: &requesterSymbol,
		IllRequest: map[string]interface{}{
			"bibliographicInfo": map[string]interface{}{
				"title": "Invalid request",
			},
			"serviceInfo": map[string]interface{}{
				"serviceType": "Broken",
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
		Timestamp: pgtype.Timestamp{
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

	respBytes := httpRequest(t, "GET", basePath+"?symbol=ISIL:REQ&side=borrowing&cal=cql.serverChoice%20all%20%22REQ-123%20P456%20Dream%20Ray%20Bradbury%20John%20Doe%20PP-789%20BAR-321%20CAL-321%20ITEM-321%22", []byte{}, 200)
	var foundPrs proapi.PatronRequests
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, prId, foundPrs.Items[0].Id)
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
