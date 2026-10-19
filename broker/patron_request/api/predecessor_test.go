package prapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type predecessorRepo struct {
	PrRepoError
	previous  pr_db.PatronRequest
	created   pr_db.PatronRequest
	updated   pr_db.PatronRequest
	loadErr   error
	createErr error
	updateErr error
}

func (r *predecessorRepo) WithTxFunc(ctx common.ExtendedContext, fn func(pr_db.PrRepo) error) error {
	return fn(r)
}

func (r *predecessorRepo) GetPatronRequestByIdForUpdate(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	return r.previous, r.loadErr
}

func (r *predecessorRepo) CreatePatronRequest(ctx common.ExtendedContext, params pr_db.CreatePatronRequestParams) (pr_db.PatronRequest, error) {
	if r.createErr != nil {
		return pr_db.PatronRequest{}, r.createErr
	}
	r.created = pr_db.PatronRequest(params)
	return r.created, nil
}

func (r *predecessorRepo) UpdatePatronRequest(ctx common.ExtendedContext, params pr_db.UpdatePatronRequestParams) (pr_db.PatronRequest, error) {
	r.updated = pr_db.PatronRequest(params)
	return r.updated, r.updateErr
}

func (r *predecessorRepo) GetPatronRequestSearchView(ctx common.ExtendedContext, id string) (pr_db.PatronRequestSearchView, error) {
	return pr_db.PatronRequestSearchView{
		ID: r.created.ID, PrevReqID: r.created.PrevReqID, State: r.created.State,
		StateModel: r.created.StateModel, Side: r.created.Side, IllRequest: r.created.IllRequest,
		CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
	}, nil
}

func TestPostPatronRequestsPredecessor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prevID string
		setup  func(*predecessorRepo)
		status int
	}{
		{"linked", "previous", nil, http.StatusCreated},
		{"empty", "", nil, http.StatusBadRequest},
		{"self", "new", nil, http.StatusBadRequest},
		{"missing", "missing", func(r *predecessorRepo) { r.loadErr = pgx.ErrNoRows }, http.StatusNotFound},
		{"other requester", "previous", func(r *predecessorRepo) { r.previous.RequesterSymbol = pgtype.Text{String: "ISIL:OTHER", Valid: true} }, http.StatusNotFound},
		{"lending", "previous", func(r *predecessorRepo) { r.previous.Side = prservice.SideLending }, http.StatusNotFound},
		{"already linked", "previous", func(r *predecessorRepo) { r.previous.NextReqID = pgtype.Text{String: "existing", Valid: true} }, http.StatusBadRequest},
		{"load error", "previous", func(r *predecessorRepo) { r.loadErr = errors.New("load failed") }, http.StatusInternalServerError},
		{"create error", "previous", func(r *predecessorRepo) { r.createErr = errors.New("create failed") }, http.StatusInternalServerError},
		{"update error", "previous", func(r *predecessorRepo) { r.updateErr = errors.New("update failed") }, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &predecessorRepo{previous: pr_db.PatronRequest{
				ID: "previous", Side: prservice.SideBorrowing,
				RequesterSymbol: pgtype.Text{String: symbol, Valid: true},
			}}
			if tc.setup != nil {
				tc.setup(repo)
			}
			handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
			id := "new"
			body, err := json.Marshal(proapi.CreatePatronRequest{
				Id: &id, RequesterSymbol: &symbol, PrevReqId: &tc.prevID, IllRequest: validIllRequest(),
			})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/patron_requests", bytes.NewReader(body))
			rr := httptest.NewRecorder()
			handler.PostPatronRequests(rr, req, proapi.PostPatronRequestsParams{})
			require.Equal(t, tc.status, rr.Code, rr.Body.String())
			if tc.status == http.StatusCreated {
				assert.Equal(t, "previous", repo.created.PrevReqID.String)
				assert.Equal(t, id, repo.updated.NextReqID.String)
				assert.Contains(t, rr.Body.String(), `"prevReqId":"previous"`)
			} else if tc.name != "update error" {
				assert.Empty(t, repo.created.ID)
				assert.Empty(t, repo.updated.ID)
			}
		})
	}
}
