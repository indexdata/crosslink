package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/jackc/pgx/v5/pgtype"
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

func TestLocatedSupplierSelectedItemResponse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/located_suppliers", nil)
	for _, id := range []pgtype.Text{{}, {String: "selected-copy", Valid: true}} {
		row := ill_db.GetLocatedSuppliersWithPeerByIllTransactionRow{LocatedSupplier: ill_db.LocatedSupplier{LocalID: pgtype.Text{String: "bib", Valid: true}, ItemID: id}}
		response := toApiLocatedSupplier(req, row)
		assert.Equal(t, "bib", *response.LocalID)
		if id.Valid {
			assert.Equal(t, id.String, *response.ItemID)
		} else {
			assert.Nil(t, response.ItemID)
		}
	}
}

func TestLocatedSupplierPolicyReadinessResponse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/located_suppliers", nil)
	for _, ready := range []pgtype.Bool{{}, {Valid: true}, {Valid: true, Bool: true}} {
		row := ill_db.GetLocatedSuppliersWithPeerByIllTransactionRow{LocatedSupplier: ill_db.LocatedSupplier{HoldingsPolicyResolved: ready}}
		response := toApiLocatedSupplier(req, row)
		if ready.Valid {
			if assert.NotNil(t, response.HoldingsPolicyResolved) {
				assert.Equal(t, ready.Bool, *response.HoldingsPolicyResolved)
			}
		} else {
			assert.Nil(t, response.HoldingsPolicyResolved)
		}
	}
}
