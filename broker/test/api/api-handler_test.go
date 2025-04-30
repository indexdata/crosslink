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
	body := getResponseBody(t, "/events")
	var resp []oapi.Event
	err := json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	if len(resp) == 0 {
		t.Errorf("Did not find events")
	}
	if resp[0].ID != eventId {
		t.Errorf("Did not find created event")
	}

	body = getResponseBody(t, "/events?ill_transaction_id="+illId)
	err = json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if len(resp) == 0 {
		t.Errorf("did not find events")
	}
	if resp[0].ID != eventId {
		t.Errorf("did not find created event")
	}
}

func TestGetIllTransactions(t *testing.T) {
	id := test.GetIllTransId(t, illRepo)
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	trans, err := illRepo.GetIllTransactionById(ctx, id)
	if err != nil {
		t.Errorf("failed to read transaction from DB: %s", err)
	}
	reqReqId := "reqReqId1"
	trans.RequesterRequestID = pgtype.Text{
		String: reqReqId,
		Valid:  true,
	}
	trans, err = illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(trans))
	if err != nil {
		t.Errorf("failed to save transaction in DB: %s", err)
	}
	body := getResponseBody(t, "/ill_transactions")
	var resp []oapi.IllTransaction
	err = json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if len(resp) == 0 {
		t.Errorf("did not find ILL transaction")
	}

	// Query
	body = getResponseBody(t, "/ill_transactions?requester_req_id="+reqReqId)
	err = json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if reqReqId != resp[0].RequesterRequestID {
		t.Errorf("expected to find with same requester request id, got: %v, expected %v", resp[0].RequesterRequestID, reqReqId)
	}
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
	// Delete peer
	req, err := http.NewRequest("DELETE", getLocalhostWithPort()+"/ill_transactions/"+illId, nil)
	if err != nil {
		t.Errorf("Error creating delete transaction request: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	delResp, err := client.Do(req)
	if err != nil {
		t.Errorf("Error deleting peer request: %s", err)
	}
	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected response 204 got %d", delResp.StatusCode)
	}
}

func TestGetLocatedSuppliers(t *testing.T) {
	illId := test.GetIllTransId(t, illRepo)
	peer := test.CreatePeer(t, illRepo, "ISIL:LOC_SUP", "")
	locSup := test.CreateLocatedSupplier(t, illRepo, illId, peer.ID, "ISIL:LOC_SUP", string(iso18626.TypeStatusLoaned))
	body := getResponseBody(t, "/located_suppliers")
	var resp []oapi.LocatedSupplier
	err := json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	if len(resp) == 0 {
		t.Errorf("Did not find located suppliers")
	}
	if resp[0].ID != locSup.ID {
		t.Errorf("Did not find created located supplier")
	}

	body = getResponseBody(t, "/located_suppliers?ill_transaction_id="+illId)
	err = json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if len(resp) == 0 {
		t.Errorf("did not find located suppliers")
	}
	if resp[0].ID != locSup.ID {
		t.Errorf("did not find created located supplier ")
	}

	body = getResponseBody(t, "/located_suppliers?ill_transaction_id=not-exists")
	err = json.Unmarshal(body, &resp)
	if err != nil {
		t.Errorf("failed to unmarshal json: %s", err)
	}
	if len(resp) > 0 {
		t.Errorf("should not find located suppliers")
	}
}

func TestBrokerCRUD(t *testing.T) {
	resp := getResponseBody(t, "/broker/peers")
	var respPeers []oapi.Peer
	err := json.Unmarshal(resp, &respPeers)
	assert.NoError(t, err)
	// assert.Len(t, respPeers, 0)
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
	req, err := http.NewRequest("POST", getLocalhostWithPort()+"/peers", bytes.NewBuffer(jsonBytes))
	if err != nil {
		t.Errorf("Error creating post peer request: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("Error posting peer request: %s", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %s", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected response 201 got %d", resp.StatusCode)
	}
	var respPeer oapi.Peer
	err = json.Unmarshal(body, &respPeer)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	if toCreate.ID != respPeer.ID {
		t.Errorf("expected same peer %s got %s", toCreate.ID, respPeer.ID)
	}
	// Cannot post same again
	resp, err = client.Do(req)
	if err != nil {
		t.Errorf("Error posting peer request: %s", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected response 400 got %d", resp.StatusCode)
	}
	// Update peer
	toCreate.Name = "Updated"
	jsonBytes, err = json.Marshal(toCreate)
	if err != nil {
		t.Errorf("Error marshaling JSON: %s", err)
	}
	req, err = http.NewRequest("PUT", getLocalhostWithPort()+"/peers/"+toCreate.ID, bytes.NewBuffer(jsonBytes))
	if err != nil {
		t.Errorf("Error creating put peer request: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Errorf("Error putting peer request: %s", err)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected response 200 got %d", resp.StatusCode)
	}
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
	if len(respPeers) < 1 {
		t.Errorf("Did not find peers")
	}
	// Query peers
	body = getResponseBody(t, "/peers?cql="+url.QueryEscape("symbol any ISIL:PEER"))
	err = json.Unmarshal(body, &respPeers)
	if err != nil {
		t.Errorf("Failed to unmarshal json: %s", err)
	}
	if toCreate.ID != respPeers[0].ID {
		t.Errorf("expected same peer %s got %s", toCreate.ID, respPeers[0].ID)
	}
	// Delete peer
	req, err = http.NewRequest("DELETE", getLocalhostWithPort()+"/peers/"+toCreate.ID, nil)
	if err != nil {
		t.Errorf("Error creating delete peer request: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Errorf("Error deleting peer request: %s", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected response 204 got %d", resp.StatusCode)
	}
	// Check no peers left
	respPeers = getPeers(t)
	for _, p := range respPeers {
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
			if tt.method == "GET" {
				resp, err := http.Get(getLocalhostWithPort() + tt.endpoint)
				if err != nil {
					t.Errorf("Error making GET request: %s", err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusNotFound {
					t.Errorf("Expected response 404 got %d", resp.StatusCode)
				}
			} else {
				req, err := http.NewRequest(tt.method, getLocalhostWithPort()+tt.endpoint, nil)
				if err != nil {
					t.Errorf("Error creating post peer request: %s", err)
				}
				req.Header.Set("Content-Type", "application/json")
				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					t.Errorf("Error doing peer request: %s", err)
				}
				if resp.StatusCode != http.StatusNotFound {
					t.Errorf("Expected response 404 got %d", resp.StatusCode)
				}
			}
		})
	}
}
func TestGetEventsDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetEvents(rr, req, oapi.GetEventsParams{})
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
	handlerMock.GetLocatedSuppliers(rr, req, oapi.GetLocatedSuppliersParams{})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func getPeers(t *testing.T) []oapi.Peer {
	body := getResponseBody(t, "/peers")
	var respPeers []oapi.Peer
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
	resp, err := http.Get(getLocalhostWithPort() + endpoint)
	if err != nil {
		t.Errorf("Error making GET request: %s", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Error reading response body: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected response 200 got %d", resp.StatusCode)
	}
	return body
}

func getLocalhostWithPort() string {
	return "http://localhost:" + strconv.Itoa(app.HTTP_PORT)
}
