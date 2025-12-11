package prapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/ill_db"
	proapi "github.com/indexdata/crosslink/broker/patron_request/oapi"
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

func TestMain(m *testing.M) {
	app.TENANT_TO_SYMBOL = "ISIL:DK-{tenant}"
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

	reqPeer := apptest.CreatePeer(t, illRepo, requesterSymbol, adapter.MOCK_CLIENT_URL)
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
	jsonBytes, err := json.Marshal(request)
	assert.NoError(t, err)
	illMessage := string(jsonBytes)
	newPr := proapi.CreatePatronRequest{
		ID:              uuid.NewString(),
		Timestamp:       time.Now(),
		SupplierSymbol:  &supplierSymbol,
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      &illMessage,
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	respBytes := httpRequest(t, "POST", basePath, newPrBytes, 201)

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, newPr.ID, foundPr.ID)
	assert.True(t, foundPr.State != "")
	assert.Equal(t, prservice.SideBorrowing, foundPr.Side)
	assert.Equal(t, newPr.Timestamp.YearDay(), foundPr.Timestamp.YearDay())
	assert.Equal(t, *newPr.RequesterSymbol, *foundPr.RequesterSymbol)
	assert.Equal(t, *newPr.SupplierSymbol, *foundPr.SupplierSymbol)
	assert.Equal(t, *newPr.Patron, *foundPr.Patron)
	assert.NotNil(t, *foundPr.IllRequest)

	// GET list
	respBytes = httpRequest(t, "GET", basePath, []byte{}, 200)
	var foundPrs []proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Len(t, foundPrs, 2)
	assert.Equal(t, newPr.ID, foundPrs[1].ID)

	// GET by id
	thisPrPath := basePath + "/" + newPr.ID
	respBytes = httpRequest(t, "GET", thisPrPath, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, newPr.ID, foundPr.ID)

	// GET actions by PR id
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\"send-request\"]\n"
	})
	respBytes = httpRequest(t, "GET", thisPrPath+"/actions", []byte{}, 200)
	assert.Equal(t, "[\"send-request\"]\n", string(respBytes))

	// POST execute action
	action := proapi.ExecuteAction{
		Action: "send-request",
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", thisPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait till requester response processed
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", thisPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\"receive\"]\n"
	})

	// POST blocking action
	action = proapi.ExecuteAction{
		Action: "receive",
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", thisPrPath+"/action", actionBytes, 200)
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
	requesterSymbol := "localISIL:REQ" + uuid.NewString()
	supplierSymbol := "localISIL:SUP" + uuid.NewString()

	reqPeer := apptest.CreatePeer(t, illRepo, requesterSymbol, adapter.MOCK_CLIENT_URL)
	assert.NotNil(t, reqPeer)
	supPeer := apptest.CreatePeer(t, illRepo, supplierSymbol, adapter.MOCK_CLIENT_URL)
	assert.NotNil(t, supPeer)

	// POST
	patron := "p1"
	request := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "return-" + supplierSymbol + "::WILLSUPPLY_LOANED",
		},
	}
	jsonBytes, err := json.Marshal(request)
	assert.NoError(t, err)
	illMessage := string(jsonBytes)
	newPr := proapi.CreatePatronRequest{
		ID:              uuid.NewString(),
		Timestamp:       time.Now(),
		SupplierSymbol:  &supplierSymbol,
		RequesterSymbol: &requesterSymbol,
		Patron:          &patron,
		IllRequest:      &illMessage,
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	respBytes := httpRequest(t, "POST", basePath, newPrBytes, 201)

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, newPr.ID, foundPr.ID)
	requesterPrPath := basePath + "/" + newPr.ID

	// Wait till action available
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\""+prservice.ActionSendRequest+"\"]\n"
	})

	action := proapi.ExecuteAction{
		Action: prservice.ActionSendRequest,
	}
	actionBytes, err := json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Find supplier patron request
	test.WaitForPredicateToBeTrue(func() bool {
		supPr, _ := prRepo.GetPatronRequestBySupplierSymbolAndRequesterReqId(appCtx, supplierSymbol, newPr.ID)
		return supPr.ID != ""
	})
	supPr, err := prRepo.GetPatronRequestBySupplierSymbolAndRequesterReqId(appCtx, supplierSymbol, newPr.ID)
	assert.NoError(t, err)
	assert.NotNil(t, supPr.ID)

	// Wait for action
	supplierPrPath := basePath + "/" + supPr.ID
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\""+prservice.ActionWillSupply+"\"]\n"
	})

	// Will supply
	action = proapi.ExecuteAction{
		Action: prservice.ActionWillSupply,
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\""+prservice.ActionShip+"\"]\n"
	})

	// Ship
	action = proapi.ExecuteAction{
		Action: prservice.ActionShip,
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\""+prservice.ActionReceive+"\"]\n"
	})

	// Receive
	action = proapi.ExecuteAction{
		Action: prservice.ActionReceive,
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\""+prservice.ActionCheckOut+"\"]\n"
	})

	// Check out
	action = proapi.ExecuteAction{
		Action: prservice.ActionCheckOut,
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\""+prservice.ActionCheckIn+"\"]\n"
	})

	// Check in
	action = proapi.ExecuteAction{
		Action: prservice.ActionCheckIn,
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", requesterPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\""+prservice.ActionShipReturn+"\"]\n"
	})

	// Ship return
	action = proapi.ExecuteAction{
		Action: prservice.ActionShipReturn,
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", requesterPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Wait for action
	test.WaitForPredicateToBeTrue(func() bool {
		respBytes = httpRequest(t, "GET", supplierPrPath+"/actions", []byte{}, 200)
		return string(respBytes) == "[\""+prservice.ActionMarkReceived+"\"]\n"
	})

	// Ship return
	action = proapi.ExecuteAction{
		Action: prservice.ActionMarkReceived,
	}
	actionBytes, err = json.Marshal(action)
	assert.NoError(t, err, "failed to marshal patron request action")
	respBytes = httpRequest(t, "POST", supplierPrPath+"/action", actionBytes, 200)
	assert.Equal(t, "{\"actionResult\":\"SUCCESS\"}\n", string(respBytes))

	// Check requester patron request done
	respBytes = httpRequest(t, "GET", requesterPrPath, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, newPr.ID, foundPr.ID)
	assert.Equal(t, prservice.BorrowerStateCompleted, foundPr.State)

	// Check supplier patron request done
	respBytes = httpRequest(t, "GET", supplierPrPath, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, supPr.ID, foundPr.ID)
	assert.Equal(t, prservice.LenderStateCompleted, foundPr.State)
}

func httpRequest(t *testing.T, method string, uriPath string, reqbytes []byte, expectStatus int) []byte {
	client := http.DefaultClient
	hreq, err := http.NewRequest(method, getLocalhostWithPort()+uriPath, bytes.NewBuffer(reqbytes))
	assert.NoError(t, err)
	hreq.Header.Set("X-Okapi-Tenant", "test-lib")

	if method == "POST" || method == "PUT" {
		hreq.Header.Set("Content-Type", "application/json")
	}
	hres, err := client.Do(hreq)
	assert.NoError(t, err)
	defer hres.Body.Close()
	body, err := io.ReadAll(hres.Body)
	assert.Equal(t, expectStatus, hres.StatusCode, string(body))
	assert.NoError(t, err)
	return body
}

func getLocalhostWithPort() string {
	return "http://localhost:" + strconv.Itoa(app.HTTP_PORT)
}
