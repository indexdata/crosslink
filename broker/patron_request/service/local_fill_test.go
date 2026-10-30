package prservice

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func (r *IllRepoMock) GetIllTransactionByRequesterRequestIdForUpdate(_ common.ExtendedContext, id pgtype.Text) (ill_db.IllTransaction, error) {
	args := r.Called(id)
	return args.Get(0).(ill_db.IllTransaction), args.Error(1)
}
func (r *IllRepoMock) GetSelectedSupplierForIllTransaction(_ common.ExtendedContext, id string) (ill_db.LocatedSupplier, error) {
	args := r.Called(id)
	return args.Get(0).(ill_db.LocatedSupplier), args.Error(1)
}
func localFillSelection(prID, symbol string) ill_db.LocatedSupplier {
	return ill_db.LocatedSupplier{ID: "selected", IllTransactionID: "ill-" + prID, SupplierSymbol: symbol, SupplierStatus: ill_db.SupplierStateSelectedPg, LocalSupplier: true, HoldingsPolicyResolved: pgtype.Bool{Bool: true, Valid: true}, LocalID: getDbText("located-bib")}
}
func expectLocalFillSelection(repo *IllRepoMock, prID, symbol string, selected ill_db.LocatedSupplier) {
	repo.On("GetIllTransactionByRequesterRequestIdForUpdate", getDbText(prID)).Return(ill_db.IllTransaction{ID: "ill-" + prID, RequesterSymbol: getDbText(symbol)}, nil)
	repo.On("GetSelectedSupplierForIllTransaction", "ill-"+prID).Return(selected, nil)
	repo.On("GetLocatedSupplierByIdForUpdate", selected.ID).Return(selected, nil)
	repo.On("GetAvailabilityAttempt", selected.IllTransactionID).Return(ill_db.AvailabilityAttempt{}, pgx.ErrNoRows).Maybe()
}

func TestLocalFillRejectsInvalidSelection(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	for _, tc := range []struct {
		name                         string
		transactionErr, selectionErr error
		wrongRequester               bool
		mutate                       func(*ill_db.LocatedSupplier)
	}{
		{name: "missing transaction", transactionErr: lookupErr},
		{name: "wrong requester", wrongRequester: true},
		{name: "missing selection", selectionErr: lookupErr},
		{name: "policy unresolved", mutate: func(s *ill_db.LocatedSupplier) { s.HoldingsPolicyResolved = pgtype.Bool{Valid: true} }},
		{name: "not local", mutate: func(s *ill_db.LocatedSupplier) { s.LocalSupplier = false }},
		{name: "wrong symbol", mutate: func(s *ill_db.LocatedSupplier) { s.SupplierSymbol = "ISIL:OTHER" }},
		{name: "wrong transaction", mutate: func(s *ill_db.LocatedSupplier) { s.IllTransactionID = "other" }},
		{name: "no longer selected", mutate: func(s *ill_db.LocatedSupplier) { s.SupplierStatus = ill_db.SupplierStateSkippedPg }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(IllRepoMock)
			pr := pr_db.PatronRequest{ID: "pr", RequesterSymbol: getDbText("ISIL:REQ"), RequesterReqID: getDbText("external-id")}
			trans := ill_db.IllTransaction{ID: "ill-pr", RequesterSymbol: pr.RequesterSymbol}
			if tc.wrongRequester {
				trans.RequesterSymbol = getDbText("ISIL:OTHER")
			}
			repo.On("GetIllTransactionByRequesterRequestIdForUpdate", getDbText(pr.ID)).Return(trans, tc.transactionErr).Once()
			if tc.transactionErr == nil && !tc.wrongRequester {
				selected := localFillSelection(pr.ID, pr.RequesterSymbol.String)
				if tc.mutate != nil {
					tc.mutate(&selected)
				}
				repo.On("GetSelectedSupplierForIllTransaction", trans.ID).Return(selected, tc.selectionErr).Once()
				if tc.selectionErr == nil {
					repo.On("GetLocatedSupplierByIdForUpdate", selected.ID).Return(selected, nil).Once()
				}
			}
			adapter := new(mockLmsAdapter)
			service := PatronRequestActionService{illRepo: repo}
			result := service.fillLocallyBorrowingRequest(appCtx, "", pr, adapter, actionParams{})
			require.Equal(t, events.EventStatusError, result.status)
			require.Equal(t, "Local supplier lookup failed", result.result.EventError.Message)
			adapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			repo.AssertExpectations(t)
		})
	}
}

func TestLocalFillRejectsSelectedItemWithTitleScope(t *testing.T) {
	for _, version := range []string{"1.0", "2.02"} {
		t.Run(version, func(t *testing.T) {
			repo := new(IllRepoMock)
			pr := pr_db.PatronRequest{ID: "pr", RequesterSymbol: getDbText("ISIL:REQ")}
			selected := localFillSelection(pr.ID, pr.RequesterSymbol.String)
			selected.ItemID = getDbText("selected-copy")
			expectLocalFillSelection(repo, pr.ID, pr.RequesterSymbol.String, selected)
			// No HTTP server exists: rejection must happen before RequestItem is sent.
			adapter, err := lms.CreateLmsAdapterNcip(dirapi.LmsConfig{Address: "https://unused.invalid/ncip", FromAgency: "broker", NcipVersion: &version, RequestItemRequestScopeType: new("Title")})
			require.NoError(t, err)
			service := PatronRequestActionService{illRepo: repo}
			result := service.fillLocallyBorrowingRequest(appCtx, "", pr, adapter, actionParams{})
			require.Equal(t, events.EventStatusError, result.status)
			require.Equal(t, "Invalid selected item scope", result.result.EventError.Message)
			require.Contains(t, result.result.EventError.Cause, "policy-selected item requires NCIP Item scope")
			repo.AssertExpectations(t)
		})
	}
}

func (r *IllRepoMock) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return fn(r)
}
func (r *IllRepoMock) GetLocatedSupplierByIdForUpdate(_ common.ExtendedContext, id string) (ill_db.LocatedSupplier, error) {
	args := r.Called(id)
	return args.Get(0).(ill_db.LocatedSupplier), args.Error(1)
}
func (r *IllRepoMock) GetAvailabilityAttempt(_ common.ExtendedContext, id string) (ill_db.AvailabilityAttempt, error) {
	args := r.Called(id)
	return args.Get(0).(ill_db.AvailabilityAttempt), args.Error(1)
}
