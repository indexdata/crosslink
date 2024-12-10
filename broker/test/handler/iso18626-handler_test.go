package handler

import (
	"bytes"
	"context"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/stretchr/testify/mock"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) CreateTransaction(ctx context.Context, params queries.CreateTransactionParams) (queries.Transaction, error) {
	return queries.Transaction{
		params.ID,
		params.Timestamp,
		params.RequesterSymbol,
		params.RequesterID,
		params.RequesterAction,
		params.SupplierSymbol,
		params.State,
		params.RequesterRequestID,
		params.SupplierRequestID,
		params.Data,
	}, nil
}

var mockRepo = new(MockRepository)

func TestIso18626PostHandlerSuccess(t *testing.T) {
	data, _ := os.ReadFile("testdata/request.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockRepo)(rr, req)

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
	data, _ := os.ReadFile("testdata/request.xml")
	req, _ := http.NewRequest("GET", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockRepo)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusMethodNotAllowed)
	}
}

func TestIso18626PostHandlerWrongContentType(t *testing.T) {
	data, _ := os.ReadFile("testdata/request.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockRepo)(rr, req)

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

	handler.Iso18626PostHandler(mockRepo)(rr, req)

	// Check the response
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusBadRequest)
	}
}
