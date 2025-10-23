package prapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	proapi "github.com/indexdata/crosslink/broker/patron_request/oapi"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetId(t *testing.T) {
	assert.True(t, getId("") != "")
	assert.Equal(t, "id1", getId("id1"))
}

func TestGetDbText(t *testing.T) {
	text := "v1"
	result := getDbText(&text)
	assert.True(t, result.Valid)
	assert.Equal(t, "v1", result.String)

	result = getDbText(nil)
	assert.False(t, result.Valid)
}

func TestGetPatronRequests(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequests(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPostPatronRequests(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	toCreate := proapi.PatronRequest{ID: "1"}
	jsonBytes, err := json.Marshal(toCreate)
	assert.NoError(t, err, "failed to marshal patron request")
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PostPatronRequests(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPostPatronRequestsInvalidJson(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer([]byte("a\": v\"")))
	rr := httptest.NewRecorder()
	handler.PostPatronRequests(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestDeletePatronRequestsIdNotFound(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2")
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNotFound)
	}
}

func TestDeletePatronRequestsId(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "3")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestGetPatronRequestsIdNotFound(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2")
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNotFound)
	}
}

func TestGetPatronRequestsId(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "1")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPutPatronRequestsIdNotFound(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	toCreate := proapi.PatronRequest{ID: "2"}
	jsonBytes, err := json.Marshal(toCreate)
	assert.NoError(t, err, "failed to marshal patron request")
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "2")
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNotFound)
	}
}

func TestPutPatronRequestsId(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	toCreate := proapi.PatronRequest{ID: "1"}
	jsonBytes, err := json.Marshal(toCreate)
	assert.NoError(t, err, "failed to marshal patron request")
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "1")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPutPatronRequestsIdSaveError(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	toCreate := proapi.PatronRequest{ID: "3"}
	jsonBytes, err := json.Marshal(toCreate)
	assert.NoError(t, err, "failed to marshal patron request")
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPutPatronRequestsIdInvalidJson(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError))
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer([]byte("a\":v\"")))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

type PrRepoError struct {
	mock.Mock
}

func (r *PrRepoError) WithTxFunc(ctx common.ExtendedContext, fn func(repo pr_db.PrRepo) error) error {
	return fn(r)
}

func (r *PrRepoError) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	switch id {
	case "2":
		return pr_db.PatronRequest{}, pgx.ErrNoRows
	case "3":
		return pr_db.PatronRequest{ID: id}, nil
	default:
		return pr_db.PatronRequest{}, errors.New("DB error")
	}
}
func (r *PrRepoError) ListPatronRequests(ctx common.ExtendedContext) ([]pr_db.PatronRequest, error) {
	return []pr_db.PatronRequest{}, errors.New("DB error")
}
func (r *PrRepoError) SavePatronRequest(ctx common.ExtendedContext, params pr_db.SavePatronRequestParams) (pr_db.PatronRequest, error) {
	return pr_db.PatronRequest{}, errors.New("DB error")
}
func (r *PrRepoError) DeletePatronRequest(ctx common.ExtendedContext, id string) error {
	return errors.New("DB error")
}
