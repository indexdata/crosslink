package handler

import (
	"bytes"
	extctx "github.com/indexdata/crosslink/broker/common"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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

func TestIso18626PostHandlerSuccess(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

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
	data, _ := os.ReadFile("../testdata/request.xml")
	req, _ := http.NewRequest("GET", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusMethodNotAllowed)
	}
}

func TestIso18626PostHandlerWrongContentType(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

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

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusBadRequest)
	}
}

func TestIso18626PostHandlerFailToLocateRequesterSymbol(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoError, eventBussError)(rr, req)

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
	data, _ := os.ReadFile("../testdata/request.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	var mockRepo = &MockRepository{}

	handler.Iso18626PostHandler(mockRepo, eventBussError)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestIso18626PostHandlerMissingRequestingId(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-invalid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

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
	data, _ := os.ReadFile("../testdata/supplying-agency-message.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

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
	data, _ := os.ReadFile("../testdata/supplying-agency-message.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoError, eventBussError)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestIso18626PostSupplyingMessageMissing(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supplying-agency-message-invalid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

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
	data, _ := os.ReadFile("../testdata/requesting-agency-message.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

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

func TestIso18626PostRequestingMessageFailedToFindIllTransaction(t *testing.T) {
	data, _ := os.ReadFile("../testdata/requesting-agency-message.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoError, eventBussError)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestIso18626PostRequestingMessageMissing(t *testing.T) {
	data, _ := os.ReadFile("../testdata/requesting-agency-message-invalid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess)(rr, req)

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

func (r *MockRepository) GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (ill_db.Peer, error) {
	return ill_db.Peer{
		ID:     "peer1",
		Name:   symbol,
		Symbol: symbol,
	}, nil
}
