package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	mockapp "github.com/indexdata/crosslink/illmock/app"
	"github.com/indexdata/go-utils/utils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test"
)

var mockIllRepoSuccess = new(test.MockIllRepositorySuccess)
var mockEventRepoSuccess = new(test.MockEventRepositorySuccess)
var eventBussSuccess = events.NewPostgresEventBus(mockEventRepoSuccess, "mock")
var mockIllRepoError = new(test.MockIllRepositoryError)
var mockEventRepoError = new(test.MockEventRepositoryError)
var eventBussError = events.NewPostgresEventBus(mockEventRepoError, "mock")
var dirAdapter = new(adapter.MockDirectoryLookupAdapter)
var illRepo ill_db.IllRepo

func TestMain(m *testing.M) {
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

	mockPort := strconv.Itoa(utils.Must(test.GetFreePort()))
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	test.Expect(os.Setenv("HTTP_PORT", mockPort), "failed to set mock client port")
	test.Expect(os.Setenv("PEER_URL", "http://localhost:"+strconv.Itoa(app.HTTP_PORT)), "failed to set peer URL")

	go func() {
		var mockApp mockapp.MockApp
		test.Expect(mockApp.Run(), "failed to start ill mock client")
	}()
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	adapter.MOCK_CLIENT_URL = "http://localhost:" + mockPort + "/iso18626"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, illRepo, _ = test.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestIso18626PostHandlerSuccess(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expected := "<messageStatus>OK</messageStatus>"
	if !strings.Contains(rr.Body.String(), expected) {
		t.Errorf("handler returned unexpected body: got %v want to contain %v",
			rr.Body.String(), expected)
	}
}

func TestIso18626PostHandlerWrongMethod(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-ok.xml")
	req, _ := http.NewRequest("GET", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusMethodNotAllowed)
	}
}

func TestIso18626PostHandlerWrongContentType(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusUnsupportedMediaType {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusUnsupportedMediaType)
	}
}

func TestIso18626PostHandlerInvalidBody(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("Invalid")))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusBadRequest)
	}
}

func TestIso18626PostHandlerFailToLocateRequesterSymbol(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoError, eventBussError, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	expected := "<messageStatus>ERROR</messageStatus>"
	if !strings.Contains(rr.Body.String(), expected) {
		t.Errorf("handler returned unexpected body: got %v want to contain %v",
			rr.Body.String(), expected)
	}
}

func TestIso18626PostHandlerFailToSave(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	var mockRepo = &MockRepository{}

	handler.Iso18626PostHandler(mockRepo, eventBussError, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestIso18626PostHandlerMissingRequestingId(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-no-reqid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expected := "<messageStatus>ERROR</messageStatus>"
	if !strings.Contains(rr.Body.String(), expected) {
		t.Errorf("handler returned unexpected body: got %v want to contain %v",
			rr.Body.String(), expected)
	}
}

func TestIso18626PostSupplyingMessage(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	expected := "<messageStatus>OK</messageStatus>"
	if !strings.Contains(rr.Body.String(), expected) {
		t.Errorf("handler returned unexpected body: got %v want to contain %v",
			rr.Body.String(), expected)
	}
}

func TestIso18626PostSupplyingMessageFailedToFind(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoError, eventBussError, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestIso18626PostSupplyingMessageMissing(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-no-reqid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	expected := "<messageStatus>ERROR</messageStatus>"
	if !strings.Contains(rr.Body.String(), expected) {
		t.Errorf("handler returned unexpected body: got %v want to contain %v",
			rr.Body.String(), expected)
	}
}

func TestIso18626PostRequestingMessage(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		contains  string
		urlEnding string
		useMock   bool
	}{
		{
			name:      "ResponseSuccessful",
			status:    200,
			contains:  "<messageStatus>OK</messageStatus>",
			urlEnding: "",
			useMock:   true,
		},
		{
			name:      "Response400",
			status:    400,
			contains:  "Bad request",
			urlEnding: "/error400",
			useMock:   true,
		},
		{
			name:      "Response500",
			status:    500,
			contains:  "Internal server error",
			urlEnding: "/error500",
			useMock:   true,
		},
		{
			name:      "ResponseBadlyFormedMessage",
			status:    200,
			contains:  "<errorType>BadlyFormedMessage</errorType>",
			urlEnding: "/notExists",
			useMock:   false,
		},
	}
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	data, _ := os.ReadFile("../testdata/reqmsg-ok.xml")
	illId := uuid.NewString()
	_, err := illRepo.SaveIllTransaction(appCtx, ill_db.SaveIllTransactionParams{
		ID:                 illId,
		Timestamp:          test.GetNow(),
		RequesterRequestID: test.CreatePgText("slnp-0009997"),
	})
	if err != nil {
		t.Errorf("failed to create ill transaction: %s", err)
	}
	peer := test.CreatePeer(t, illRepo, "isil:reqTest", adapter.MOCK_CLIENT_URL)
	test.CreateLocatedSupplier(t, illRepo, illId, peer.ID, "selected")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.useMock {
				peer.Url = adapter.MOCK_CLIENT_URL + tt.urlEnding
			} else {
				port, _ := test.GetFreePort()
				peer.Url = "http:localhost:" + strconv.Itoa(port) + tt.urlEnding
			}
			peer, err = illRepo.SavePeer(appCtx, ill_db.SavePeerParams(peer))
			if err != nil {
				t.Errorf("failed to update peer : %s", err)
			}
			url := "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
			req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
			req.Header.Add("Content-Type", "application/xml")
			client := &http.Client{}
			res, err := client.Do(req)
			if err != nil {
				t.Errorf("failed to send request to broker :%s", err)
			}
			if res.StatusCode != tt.status {
				t.Errorf("handler returned wrong status code: got %v want %v",
					res.StatusCode, tt.status)
			}
			body, _ := io.ReadAll(res.Body)
			if !strings.Contains(string(body), tt.contains) {
				t.Errorf("handler returned unexpected body: got %v want to contain %v",
					string(body), tt.contains)
			}
		})
	}
}

func TestIso18626PostRequestingMessageFailedToFindIllTransaction(t *testing.T) {
	data, _ := os.ReadFile("../testdata/reqmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoError, eventBussError, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestIso18626PostRequestingMessageMissing(t *testing.T) {
	data, _ := os.ReadFile("../testdata/reqmsg-no-reqid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	expected := "<messageStatus>ERROR</messageStatus>"
	if !strings.Contains(rr.Body.String(), expected) {
		t.Errorf("handler returned unexpected body: got %v want to contain %v",
			rr.Body.String(), expected)
	}
}

type MockRepository struct {
	test.MockIllRepositoryError
}

func (r *MockRepository) GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) []ill_db.Peer {
	return []ill_db.Peer{{
		ID:     "peer1",
		Name:   symbols[0],
		Symbol: symbols[0],
	}}
}
