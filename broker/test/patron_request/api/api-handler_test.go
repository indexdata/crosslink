package prapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
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
	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_CLIENT_URL, app.BROKER_MODE, common.VendorCrossLink,
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
		},
	}
	id := uuid.NewString()
	newPr := proapi.CreatePatronRequest{
		Id:              &id,
		SupplierSymbol:  &supplierSymbol,
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
	assert.True(t, strings.HasSuffix(location, "/patron_requests/"+id), "Location header should end with /patron_requests/{id}")

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, *newPr.Id, foundPr.Id)
	assert.True(t, foundPr.State != "")
	assert.Equal(t, string(prservice.SideBorrowing), foundPr.Side)
	assert.Equal(t, *newPr.RequesterSymbol, *foundPr.RequesterSymbol)
	assert.Equal(t, *newPr.SupplierSymbol, *foundPr.SupplierSymbol)
	assert.Equal(t, *newPr.Patron, *foundPr.Patron)

	respBytes = httpRequest(t, "POST", basePath, newPrBytes, 400)
	assert.Contains(t, string(respBytes), "a patron request with the same requester_request_id already exists for this requester")

	// GET list
	queryParams := "?side=borrowing&symbol=" + *foundPr.RequesterSymbol
	respBytes = httpRequest(t, "GET", basePath+queryParams, []byte{}, 200)
	var foundPrs proapi.PatronRequests
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Equal(t, *newPr.Id, foundPrs.Items[0].Id)

	// GET list with offset in
	respBytes = httpRequest(t, "GET", basePath+queryParams+"&offset=100000", []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, int64(1), foundPrs.About.Count)
	assert.Len(t, foundPrs.Items, 0)

	// GET by id with symbol and side
	thisPrPath := basePath + "/" + *newPr.Id
	respBytes = httpRequest(t, "GET", thisPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)

	// GET by id with symbol
	respBytes = httpRequest(t, "GET", thisPrPath+"?symbol="+*foundPr.RequesterSymbol, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, *newPr.Id, foundPr.Id)

	// GET actions by PR id
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
		return string(respBytes) == "[\"send-request\"]\n"
	})
	respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
	assert.Equal(t, "[\"send-request\"]\n", string(respBytes))

	// POST execute action
	action := proapi.ExecuteAction{
		Action: "send-request",
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait till requester response processed
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions"+queryParams, []byte{}, 200)
		return string(respBytes) == "[\"receive\"]\n"
	})

	// POST blocking action
	action = proapi.ExecuteAction{
		Action: "receive",
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", thisPrPath+"/action"+queryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// TODO Do we really want to delete from DB or just add DELETED status ?
	//// DELETE patron request
	//httpRequest(t, "DELETE", thisPrPath, []byte{}, 204)
	//
	//// GET patron request which is deleted
	//httpRequest(t, "DELETE", thisPrPath, []byte{}, 404)
}

func TestActionsToCompleteState(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	requesterSymbol := "ISIL:REQ" + uuid.NewString()
	supplierSymbol := "ISIL:SUP" + uuid.NewString()

	reqPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, requesterSymbol, adapter.MOCK_CLIENT_URL, app.BROKER_MODE, common.VendorCrossLink, directory.Entry{})
	assert.NotNil(t, reqPeer)

	lmsConfig := &directory.LmsConfig{
		FromAgency: "from-agency",
		Address:    ncipMockUrl,
	}
	supPeer := apptest.CreatePeerWithModeAndVendor(t, illRepo, supplierSymbol, adapter.MOCK_CLIENT_URL, app.BROKER_MODE, common.VendorCrossLink,
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
		SupplierSymbol:  &supplierSymbol,
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
		return string(respBytes) == "[\""+string(prservice.BorrowerActionSendRequest)+"\"]\n"
	})

	action := proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionSendRequest),
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Find supplier patron request
	test.WaitForPredicateToBeTrue(func() bool {
		supPr, _ := prRepo.GetPatronRequestBySupplierSymbolAndRequesterReqId(appCtx, supplierSymbol, foundPr.Id)
		return supPr.ID != ""
	})
	supPr, err := prRepo.GetPatronRequestBySupplierSymbolAndRequesterReqId(appCtx, supplierSymbol, foundPr.Id)
	assert.NoError(t, err)
	assert.NotNil(t, supPr.ID)

	// Wait for action
	supplierPrPath := basePath + "/" + supPr.ID
	supQueryParams := "?side=lending&symbol=" + *foundPr.SupplierSymbol
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions"+supQueryParams, []byte{}, 200)
		return string(respBytes) == "[\""+string(prservice.LenderActionWillSupply)+"\"]\n"
	})

	// Will supply
	action = proapi.ExecuteAction{
		Action: string(prservice.LenderActionWillSupply),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action"+supQueryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions"+supQueryParams, []byte{}, 200)
		return string(respBytes) == "[\""+string(prservice.LenderActionShip)+"\"]\n"
	})

	// Ship
	action = proapi.ExecuteAction{
		Action: string(prservice.LenderActionShip),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action"+supQueryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return string(respBytes) == "[\""+string(prservice.BorrowerActionReceive)+"\"]\n"
	})

	// Receive
	action = proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionReceive),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return string(respBytes) == "[\""+string(prservice.BorrowerActionCheckOut)+"\"]\n"
	})

	// Check out
	action = proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionCheckOut),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return string(respBytes) == "[\""+string(prservice.BorrowerActionCheckIn)+"\"]\n"
	})

	// Check in
	action = proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionCheckIn),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions"+queryParams, []byte{}, 200)
		return string(respBytes) == "[\""+string(prservice.BorrowerActionShipReturn)+"\"]\n"
	})

	// Ship return
	action = proapi.ExecuteAction{
		Action: string(prservice.BorrowerActionShipReturn),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action"+queryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions"+supQueryParams, []byte{}, 200)
		return string(respBytes) == "[\""+string(prservice.LenderActionMarkReceived)+"\"]\n"
	})

	// Ship return
	action = proapi.ExecuteAction{
		Action: string(prservice.LenderActionMarkReceived),
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action"+supQueryParams, actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Check requester patron request done
	respBytes = httpRequest(t, "GET", requesterPrPath+queryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, string(prservice.BorrowerStateCompleted), foundPr.State)

	// Check requester patron request event count
	respBytes = httpRequest(t, "GET", requesterPrPath+"/events"+queryParams, []byte{}, 200)
	var events []oapi.Event
	err = json.Unmarshal(respBytes, &events)
	assert.NoError(t, err, "failed to unmarshal patron request events")
	assert.True(t, len(events) > 5)

	// Check supplier patron request done
	respBytes = httpRequest(t, "GET", supplierPrPath+supQueryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, supPr.ID, foundPr.Id)
	assert.Equal(t, string(prservice.LenderStateCompleted), foundPr.State)

	// Check supplier patron request event count
	respBytes = httpRequest(t, "GET", supplierPrPath+"/events"+supQueryParams, []byte{}, 200)
	err = json.Unmarshal(respBytes, &events)
	assert.NoError(t, err, "failed to unmarshal patron request events")
	assert.True(t, len(events) > 5)
}

func TestGetReturnableStateModel(t *testing.T) {
	respBytes := httpRequest(t, "GET", "/state_model/models/returnables", []byte{}, 200)
	var retrievedStateModel proapi.StateModel
	err := json.Unmarshal(respBytes, &retrievedStateModel)
	assert.NoError(t, err, "failed to unmarshal state model")
	returnablesStateModel, _ := prservice.LoadStateModelByName("returnables")
	assert.Equal(t, returnablesStateModel.Name, retrievedStateModel.Name)
	assert.Equal(t, returnablesStateModel.Desc, retrievedStateModel.Desc)
	assert.Equal(t, len(*returnablesStateModel.States), len(*retrievedStateModel.States))
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
