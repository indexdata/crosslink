package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/indexdata/crosslink/broker/test"
	"github.com/indexdata/go-utils/utils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var eventBus events.EventBus
var illRepo ill_db.IllRepo
var eventRepo events.EventRepo
var mockIllRepoError = new(test.MockIllRepositoryError)
var mockEventRepoError = new(test.MockEventRepositoryError)
var handlerMock = api.NewApiHandler(mockEventRepoError, mockIllRepoError, "")

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
	app.MigrationsFolder = "file://../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())

	ctx, cancel := context.WithCancel(context.Background())
	eventBus, illRepo, eventRepo = test.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestGetEvents(t *testing.T) {
	illId := test.GetIllTransId(t, illRepo)
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess, events.EventNameMessageRequester)

	httpGet(t, "/events", "", http.StatusBadRequest)

	body := getResponseBody(t, "/events?ill_transaction_id="+illId)
	var resp oapi.Events
	err := json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if len(resp.Items) == 0 {
		t.Errorf("did not find events")
	}
	if resp.Items[0].ID != eventId {
		t.Errorf("did not find created event")
	}

	body = getResponseBody(t, "/events?ill_transaction_id=not-exists")
	err = json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if len(resp.Items) > 0 {
		t.Errorf("should not find events")
	}
}

func TestGetIllTransactions(t *testing.T) {
	id := test.GetIllTransId(t, illRepo)
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	trans, err := illRepo.GetIllTransactionById(ctx, id)
	assert.NoError(t, err)
	reqReqId := uuid.NewString()
	trans.RequesterRequestID = pgtype.Text{
		String: reqReqId,
		Valid:  true,
	}
	trans, err = illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(trans))
	assert.NoError(t, err)
	body := getResponseBody(t, "/ill_transactions")
	var resp oapi.IllTransactions
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Items), 1)
	assert.Equal(t, resp.ResultInfo.Count, int64(len(resp.Items)))
	// Query
	body = getResponseBody(t, "/ill_transactions?requester_req_id="+url.QueryEscape(reqReqId))
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, reqReqId, resp.Items[0].RequesterRequestID)

	body = getResponseBody(t, "/ill_transactions?requester_req_id=not-exists")
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 0)

	for range 2 * api.LIMIT_DEFAULT {
		test.GetIllTransId(t, illRepo)
	}
	body = getResponseBody(t, "/ill_transactions")
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Items), int(api.LIMIT_DEFAULT))
	assert.GreaterOrEqual(t, resp.ResultInfo.Count, int64(1+2*api.LIMIT_DEFAULT))
}

func TestGetIllTransactionsId(t *testing.T) {
	illId := test.GetIllTransId(t, illRepo)
	body := getResponseBody(t, "/ill_transactions/"+illId)
	var resp oapi.IllTransaction
	err := json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if resp.ID != illId {
		t.Errorf("did not find the same ILL transaction")
	}
	assert.Equal(t, getLocalhostWithPort()+"/events?ill_transaction_id="+url.PathEscape(illId), resp.EventsLink)
	assert.Equal(t, getLocalhostWithPort()+"/located_suppliers?ill_transaction_id="+url.PathEscape(illId), resp.LocatedSuppliersLink)

	// Delete peer
	httpRequest(t, "DELETE", "/ill_transactions/"+illId, nil, "", http.StatusNoContent)
	httpRequest(t, "DELETE", "/ill_transactions/"+illId, nil, "", http.StatusNotFound)
}

func TestGetLocatedSuppliers(t *testing.T) {
	illId := test.GetIllTransId(t, illRepo)
	peer := test.CreatePeer(t, illRepo, "ISIL:LOC_SUP", "")
	locSup := test.CreateLocatedSupplier(t, illRepo, illId, peer.ID, "ISIL:LOC_SUP", string(iso18626.TypeStatusLoaned))
	httpGet(t, "/located_suppliers", "", http.StatusBadRequest)
	var resp oapi.LocatedSuppliers
	body := getResponseBody(t, "/located_suppliers?ill_transaction_id="+illId)
	err := json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if len(resp.Items) == 0 {
		t.Errorf("did not find located suppliers")
	}
	if resp.Items[0].ID != locSup.ID {
		t.Errorf("did not find created located supplier ")
	}

	body = getResponseBody(t, "/located_suppliers?ill_transaction_id=not-exists")
	err = json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if len(resp.Items) > 0 {
		t.Errorf("should not find located suppliers")
	}
}

func TestBrokerCRUD(t *testing.T) {
	// app.TENANT_TO_SYMBOL = "ISIL:DK-{tenant}"
	illId := uuid.New().String()
	reqReqId := uuid.New().String()
	_, err := illRepo.SaveIllTransaction(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
		ID: illId,
		RequesterSymbol: pgtype.Text{
			String: "ISIL:DK-DIKU",
			Valid:  true,
		},
		RequesterRequestID: pgtype.Text{
			String: reqReqId,
			Valid:  true,
		},
		Timestamp: test.GetNow(),
	})
	assert.NoError(t, err)

	body := httpGet(t, "/broker/ill_transactions/"+illId, "diku", http.StatusOK)
	var tran oapi.IllTransaction
	err = json.Unmarshal(body, &tran)
	assert.NoError(t, err)
	assert.Equal(t, illId, tran.ID)
	assert.Equal(t, getLocalhostWithPort()+"/broker/events?ill_transaction_id="+url.PathEscape(illId), tran.EventsLink)
	assert.Equal(t, getLocalhostWithPort()+"/broker/located_suppliers?ill_transaction_id="+url.PathEscape(illId), tran.LocatedSuppliersLink)

	httpGet(t, "/broker/ill_transactions/"+illId+"?requester_symbol="+url.QueryEscape("ISIL:DK-DIKU"), "diku", http.StatusOK)
	httpGet(t, "/broker/ill_transactions/"+illId+"?requester_symbol="+url.QueryEscape("ISIL:DK-DIKU"), "ruc", http.StatusNotFound)
	httpGet(t, "/broker/ill_transactions/"+illId, "ruc", http.StatusNotFound)
	httpGet(t, "/broker/ill_transactions/"+illId, "", http.StatusNotFound)

	body = httpGet(t, "/broker/ill_transactions/"+illId+"?requester_symbol="+url.QueryEscape("ISIL:DK-DIKU"), "", http.StatusOK)
	err = json.Unmarshal(body, &tran)
	assert.NoError(t, err)
	assert.Equal(t, illId, tran.ID)

	assert.Equal(t, 1, len(httpGetTrans(t, "/broker/ill_transactions", "diku", http.StatusOK)))

	assert.Equal(t, 0, len(httpGetTrans(t, "/broker/ill_transactions", "ruc", http.StatusOK)))

	body = httpGet(t, "/broker/ill_transactions?requester_req_id="+url.QueryEscape(reqReqId), "diku", http.StatusOK)
	var resp oapi.IllTransactions
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 1)
	assert.Equal(t, illId, resp.Items[0].ID)

	peer := test.CreatePeer(t, illRepo, "ISIL:LOC_OTHER", "")
	locSup := test.CreateLocatedSupplier(t, illRepo, illId, peer.ID, "ISIL:LOC_OTHER", string(iso18626.TypeStatusLoaned))

	body = httpGet(t, "/broker/located_suppliers?requester_req_id="+url.QueryEscape(reqReqId), "diku", http.StatusOK)
	var supps oapi.LocatedSuppliers
	err = json.Unmarshal(body, &supps)
	assert.NoError(t, err)
	assert.Len(t, supps.Items, 1)
	assert.Equal(t, locSup.ID, supps.Items[0].ID)

	body = httpGet(t, "/broker/located_suppliers?ill_transaction_id="+url.QueryEscape(illId), "diku", http.StatusOK)
	err = json.Unmarshal(body, &supps)
	assert.NoError(t, err)
	assert.Len(t, supps.Items, 1)
	assert.Equal(t, locSup.ID, supps.Items[0].ID)

	body = httpGet(t, "/broker/located_suppliers?requester_req_id="+url.QueryEscape(reqReqId), "ruc", http.StatusOK)
	err = json.Unmarshal(body, &supps)
	assert.NoError(t, err)
	assert.Len(t, supps.Items, 0)

	body = httpGet(t, "/broker/located_suppliers?requester_req_id="+url.QueryEscape(uuid.NewString()), "diku", http.StatusOK)
	err = json.Unmarshal(body, &supps)
	assert.NoError(t, err)
	assert.Len(t, supps.Items, 0)

	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess, events.EventNameMessageRequester)

	body = httpGet(t, "/broker/events?requester_req_id="+url.QueryEscape(reqReqId), "diku", http.StatusOK)
	var events oapi.Events
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 1)
	assert.Equal(t, eventId, events.Items[0].ID)

	body = httpGet(t, "/broker/events?requester_req_id="+url.QueryEscape(reqReqId)+"&requester_symbol="+url.QueryEscape("ISIL:DK-DIKU"), "", http.StatusOK)
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 1)
	assert.Equal(t, eventId, events.Items[0].ID)

	body = httpGet(t, "/broker/events?ill_transaction_id="+url.QueryEscape(illId), "diku", http.StatusOK)
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 1)
	assert.Equal(t, eventId, events.Items[0].ID)

	body = httpGet(t, "/broker/events?requester_req_id="+url.QueryEscape(reqReqId), "ruc", http.StatusOK)
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 0)

	body = httpGet(t, "/broker/events?requester_req_id="+url.QueryEscape(uuid.NewString()), "diku", http.StatusOK)
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 0)
}

func TestPeersCRUD(t *testing.T) {
	// Create peer
	toCreate := oapi.Peer{
		ID:            uuid.New().String(),
		Name:          "Peer",
		Url:           "https://url.com",
		Symbols:       []string{"ISIL:PEER"},
		RefreshPolicy: oapi.Transaction,
	}
	jsonBytes, err := json.Marshal(toCreate)
	if err != nil {
		t.Errorf("Error marshaling JSON: %s", err)
	}
	body := httpRequest(t, "POST", "/peers", jsonBytes, "", http.StatusCreated)
	var respPeer oapi.Peer
	err = json.Unmarshal(body, &respPeer)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	if toCreate.ID != respPeer.ID {
		t.Errorf("expected same peer %s got %s", toCreate.ID, respPeer.ID)
	}
	// Cannot post same again
	httpRequest(t, "POST", "/peers", jsonBytes, "", http.StatusBadRequest)

	// Update peer
	toCreate.Name = "Updated"
	jsonBytes, err = json.Marshal(toCreate)
	if err != nil {
		t.Errorf("Error marshaling JSON: %s", err)
	}
	body = httpRequest(t, "PUT", "/peers/"+toCreate.ID, jsonBytes, "", http.StatusOK)

	err = json.Unmarshal(body, &respPeer)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	if toCreate.ID != respPeer.ID {
		t.Errorf("expected same peer %s got %s", toCreate.ID, respPeer.ID)
	}

	if respPeer.Name != "Updated" {
		t.Errorf("expected same peer name 'Updated' got %s", respPeer.Name)
	}
	// Get peer
	respPeer = getPeerById(t, toCreate.ID)
	if toCreate.ID != respPeer.ID {
		t.Errorf("expected same peer %s got %s", toCreate.ID, respPeer.ID)
	}
	// Get peers
	respPeers := getPeers(t)
	if len(respPeers.Items) < 1 {
		t.Errorf("Did not find peers")
	}
	// Query peers
	body = getResponseBody(t, "/peers?cql="+url.QueryEscape("symbol any ISIL:PEER"))
	err = json.Unmarshal(body, &respPeers)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	if toCreate.ID != respPeers.Items[0].ID {
		t.Errorf("expected same peer %s got %s", toCreate.ID, respPeers.Items[0].ID)
	}
	// Delete peer
	httpRequest(t, "DELETE", "/peers/"+toCreate.ID, nil, "", http.StatusNoContent)
	httpRequest(t, "DELETE", "/peers/"+toCreate.ID, nil, "", http.StatusNotFound)

	// Check no peers left
	respPeers = getPeers(t)
	for _, p := range respPeers.Items {
		if p.ID == toCreate.ID {
			t.Errorf("Expected this peer %s to be deleted", toCreate.ID)
		}
	}
}

func TestNotFound(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		method   string
	}{
		{
			name:     "peers",
			endpoint: "/peers/not_found",
			method:   "GET",
		},
		{
			name:     "illTransaction",
			endpoint: "/ill_transactions/not_found",
			method:   "GET",
		},
		{
			name:     "peersPut",
			endpoint: "/peers/not_found",
			method:   "PUT",
		},
		{
			name:     "peersDelete",
			endpoint: "/peers/not_found",
			method:   "DELETE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpRequest(t, tt.method, tt.endpoint, nil, "", http.StatusNotFound)
		})
	}
}
func TestGetEventsDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	reqId := uuid.New().String()
	handlerMock.GetEvents(rr, req, oapi.GetEventsParams{RequesterReqId: &reqId})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetIllTransactionsDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetIllTransactions(rr, req, oapi.GetIllTransactionsParams{})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetIllTransactionsIdDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetIllTransactionsId(rr, req, "id", oapi.GetIllTransactionsIdParams{})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetPeersDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetPeers(rr, req, oapi.GetPeersParams{})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestPostPeersDbError(t *testing.T) {
	toCreate := oapi.Peer{
		ID:            uuid.New().String(),
		Name:          "Peer",
		Url:           "https://url.com",
		Symbols:       []string{"ISIL:PEER"},
		RefreshPolicy: oapi.Transaction,
	}
	jsonBytes, err := json.Marshal(toCreate)
	if err != nil {
		t.Errorf("Error marshaling JSON: %s", err)
	}
	req, _ := http.NewRequest("GET", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handlerMock.PostPeers(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPostPeersError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", bytes.NewBuffer([]byte{}))
	rr := httptest.NewRecorder()
	handlerMock.PostPeers(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestDeletePeersSymbolDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.DeletePeersId(rr, req, "s")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetPeersSymbolDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetPeersId(rr, req, "s")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestPutPeersSymbolDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.PutPeersId(rr, req, "s")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetLocatedSuppliersDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	reqReqId := uuid.New().String()
	handlerMock.GetLocatedSuppliers(rr, req, oapi.GetLocatedSuppliersParams{RequesterReqId: &reqReqId})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func getPeers(t *testing.T) oapi.Peers {
	body := getResponseBody(t, "/peers")
	var respPeers oapi.Peers
	err := json.Unmarshal(body, &respPeers)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	return respPeers
}

func getPeerById(t *testing.T, symbol string) oapi.Peer {
	body := getResponseBody(t, "/peers/"+symbol)
	var resp oapi.Peer
	err := json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	return resp
}

func getResponseBody(t *testing.T, endpoint string) []byte {
	return httpGet(t, endpoint, "", http.StatusOK)
}

func httpRequest(t *testing.T, method string, uriPath string, reqbytes []byte, tenant string, expectStatus int) []byte {
	client := http.DefaultClient
	hreq, err := http.NewRequest(method, getLocalhostWithPort()+uriPath, bytes.NewBuffer(reqbytes))
	assert.NoError(t, err)
	if tenant != "" {
		hreq.Header.Set("X-Okapi-Tenant", tenant)
	}
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

func httpGetTrans(t *testing.T, uriPath string, tenant string, expectStatus int) []oapi.IllTransaction {
	body := httpRequest(t, "GET", uriPath, nil, tenant, expectStatus)
	var res oapi.IllTransactions
	err := json.Unmarshal(body, &res)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	return res.Items
}

func httpGet(t *testing.T, uriPath string, tenant string, expectStatus int) []byte {
	return httpRequest(t, "GET", uriPath, nil, tenant, expectStatus)
}

func getLocalhostWithPort() string {
	return "http://localhost:" + strconv.Itoa(app.HTTP_PORT)
}
