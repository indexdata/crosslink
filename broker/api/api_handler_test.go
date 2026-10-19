package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/stretchr/testify/assert"
)

func TestGetEventsRejectsSyntheticIDs(t *testing.T) {
	for _, id := range []string{events.DEFAULT_ILL_TRANSACTION_ID, events.DEFAULT_PATRON_REQUEST_ID} {
		t.Run(id, func(t *testing.T) {
			h := ApiHandler{}
			req := httptest.NewRequest(http.MethodGet, "/events", nil)
			rr := httptest.NewRecorder()
			h.GetEvents(rr, req, oapi.GetEventsParams{IllTransactionId: &id})
			assert.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
}

func TestGetIllTransactionsIdEventsRejectsSyntheticIDs(t *testing.T) {
	for _, id := range []string{events.DEFAULT_ILL_TRANSACTION_ID, events.DEFAULT_PATRON_REQUEST_ID} {
		t.Run(id, func(t *testing.T) {
			h := ApiHandler{}
			req := httptest.NewRequest(http.MethodGet, "/ill_transactions/"+id+"/events", nil)
			rr := httptest.NewRecorder()
			h.GetIllTransactionsIdEvents(rr, req, id, oapi.GetIllTransactionsIdEventsParams{})
			assert.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
}

func TestDeleteIllTransactionsIdRejectsSyntheticID(t *testing.T) {
	h := ApiHandler{}
	req := httptest.NewRequest(http.MethodDelete, "/ill_transactions/"+events.DEFAULT_ILL_TRANSACTION_ID, nil)
	rr := httptest.NewRecorder()

	h.DeleteIllTransactionsId(rr, req, events.DEFAULT_ILL_TRANSACTION_ID)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "synthetic IDs cannot be deleted")
}
