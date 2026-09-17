package prservice

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Rollback-capable test double; PostgreSQL rollback and commit failures are also
// exercised by the repository integration tests.
type loanTxRepo struct {
	*MockPrRepo
	failStep     string
	inTx         bool
	transactions int
}

func (r *loanTxRepo) WithTxFunc(ctx common.ExtendedContext, fn func(pr_db.PrRepo) error) error {
	pr, items, ids := r.savedPr, slices.Clone(r.savedItems), maps.Clone(r.itemLmsRequestIDs)
	r.inTx = true
	r.transactions++
	err := fn(r)
	if err == nil && r.failStep == "commit" {
		err = errors.New("commit failed")
	}
	r.inTx = false
	if err != nil {
		r.savedPr, r.savedItems, r.itemLmsRequestIDs = pr, items, ids
	}
	return err
}

func (r *loanTxRepo) SetItemLmsStatus(ctx common.ExtendedContext, params pr_db.SetItemLmsStatusParams) error {
	if r.inTx && r.failStep == "status" {
		return errors.New("status failed")
	}
	return r.MockPrRepo.SetItemLmsStatus(ctx, params)
}

func (r *loanTxRepo) SetItemLmsRequestID(ctx common.ExtendedContext, params pr_db.SetItemLmsRequestIDParams) error {
	if r.inTx && r.failStep == "identifiers" {
		return errors.New("identifiers failed")
	}
	return r.MockPrRepo.SetItemLmsRequestID(ctx, params)
}

func (r *loanTxRepo) UpdatePatronRequest(ctx common.ExtendedContext, params pr_db.UpdatePatronRequestParams) (pr_db.PatronRequest, error) {
	if r.inTx && r.failStep == "date" {
		return pr_db.PatronRequest{}, errors.New("date failed")
	}
	return r.MockPrRepo.UpdatePatronRequest(ctx, params)
}

func TestReceivePersistenceFailuresCompensateAndAllowRetry(t *testing.T) {
	for _, step := range []string{"status", "identifiers", "commit"} {
		t.Run(step, func(t *testing.T) {
			pr := testLoan()
			pr.Side, pr.State = SideBorrowing, BorrowerStateShipped
			repo := &loanTxRepo{MockPrRepo: &MockPrRepo{savedPr: pr, savedItems: []pr_db.Item{{ID: "item", PrID: pr.ID, Barcode: "barcode", LmsStatus: pr_db.LmsStatusUnknown}}}, failStep: step}
			adapter := new(mockLmsAdapter)
			adapter.On("AcceptItem", "barcode", pr.ID, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
			adapter.On("DeleteItem", "barcode").Return(nil).Once()
			bus, sender := new(MockEventBus), new(MockIso18626Handler)
			svc := CreatePatronRequestActionService(repo, nil, bus, sender, nil, nil, nil, nil)
			result := svc.receiveBorrowingRequest(appCtx, "event", pr, adapter, pr.IllRequest)
			require.Equal(t, events.EventStatusError, result.status)
			assert.Equal(t, pr_db.LmsStatusDeleted, repo.savedItems[0].LmsStatus)
			assert.False(t, repo.savedItems[0].LmsRequestID.Valid)
			assert.False(t, repo.savedItems[0].LmsItemID.Valid)
			assert.Nil(t, sender.lastRequestingAgencyMessage)
			assert.Empty(t, bus.createdNoticeData)
			repo.failStep = ""
			result = svc.receiveBorrowingRequest(appCtx, "retry", pr, adapter, pr.IllRequest)
			require.Equal(t, events.EventStatusSuccess, result.status)
			assert.Equal(t, pr_db.LmsStatusAccepted, repo.savedItems[0].LmsStatus)
			assert.Equal(t, getDbText(pr.ID), repo.savedItems[0].LmsRequestID)
			// A delivery retry must not repeat the confirmed acceptance.
			result = svc.receiveBorrowingRequest(appCtx, "delivery-retry", pr, adapter, pr.IllRequest)
			require.Equal(t, events.EventStatusSuccess, result.status)
			assert.Equal(t, 2, repo.transactions)
			adapter.AssertExpectations(t)
		})
	}
}

func TestSupplierRequestItemPersistenceIsAtomic(t *testing.T) {
	for _, step := range []string{"item", "status", "commit"} {
		t.Run(step, func(t *testing.T) {
			pr := testLoan()
			pr.IllRequest.Header.RequestingAgencyRequestId = "req-1"
			repo := &loanTxRepo{MockPrRepo: &MockPrRepo{savedPr: pr, saveItemFail: step == "item"}, failStep: step}
			adapter := new(mockLmsAdapter)
			adapter.On("RequestItem", "req-1", "", "", "", "").Return(&lms.RequestedItem{RequestID: "lms-req-1", Barcode: "barcode"}, nil).Twice()
			adapter.On("CancelRequestItem", "lms-req-1", "").Return(nil).Once()
			svc := &PatronRequestActionService{prRepo: repo}

			result := svc.requestItemLenderRequest(appCtx, pr, adapter, pr.IllRequest)
			require.Equal(t, events.EventStatusError, result.status)
			assert.Empty(t, repo.savedItems, "failed transaction must not leave a reservation identifier without its status")
			assert.Equal(t, 1, repo.transactions)
			adapter.AssertNumberOfCalls(t, "CancelRequestItem", 1)

			repo.failStep, repo.saveItemFail = "", false
			result = svc.requestItemLenderRequest(appCtx, pr, adapter, pr.IllRequest)
			require.Equal(t, events.EventStatusSuccess, result.status)
			require.Len(t, repo.savedItems, 1)
			assert.Equal(t, getDbText("lms-req-1"), repo.savedItems[0].LmsRequestID)
			assert.Equal(t, pr_db.LmsStatusRequested, repo.savedItems[0].LmsStatus)

			result = svc.requestItemLenderRequest(appCtx, pr, adapter, pr.IllRequest)
			require.Equal(t, events.EventStatusSuccess, result.status)
			assert.Equal(t, 2, repo.transactions, "retry after success must not repeat the reservation")
			adapter.AssertExpectations(t)
		})
	}
}

func TestSupplierItemEditsInvalidateDatesAtomically(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, failure := range []string{"", "item", "date", "commit"} {
			t.Run(map[bool]string{false: "add", true: "remove"}[remove]+"/"+failure, func(t *testing.T) {
				pr := testLoan()
				old := time.Now().UTC().AddDate(0, 0, 10)
				pr.DueAt = pgtype.Timestamptz{Time: old, Valid: true}
				pr.IllResponse.StatusInfo.DueDate = isoLoanDate(pr.DueAt)
				items := []pr_db.Item{{ID: "original", PrID: pr.ID, Barcode: "original", LmsStatus: pr_db.LmsStatusCheckedOut, LmsDueDate: pr.DueAt}}
				if remove {
					items = append(items, pr_db.Item{ID: "removed", PrID: pr.ID, Barcode: "removed", LmsStatus: pr_db.LmsStatusUnknown})
				}
				repo := &loanTxRepo{MockPrRepo: &MockPrRepo{savedPr: pr, savedItems: slices.Clone(items)}, failStep: failure}
				repo.saveItemFail, repo.deleteItemFail = failure == "item", failure == "item"
				svc := CreatePatronRequestActionService(repo, new(IllRepoMock), new(MockEventBus), new(MockIso18626Handler), nil, nil, nil, nil)
				var result actionExecutionResult
				if remove {
					result = svc.removeItemLenderRequest(appCtx, pr, actionParams{Barcode: "removed"}, &lms.LmsAdapterManual{})
				} else {
					result = svc.addItemLenderRequest(appCtx, pr, actionParams{Barcode: "added"})
				}
				if failure != "" {
					require.Equal(t, events.EventStatusError, result.status)
					assert.Equal(t, items, repo.savedItems)
					assert.Equal(t, pr.DueAt, repo.savedPr.DueAt)
					assert.Equal(t, pr.DueAt, result.pr.DueAt)
					return
				}
				require.Equal(t, events.EventStatusSuccess, result.status)
				assert.False(t, result.pr.DueAt.Valid)
				assert.False(t, repo.savedPr.DueAt.Valid)
				assert.Nil(t, result.pr.IllResponse.StatusInfo.DueDate)
				assert.Equal(t, pr_db.LmsStatusCheckedOut, repo.savedItems[0].LmsStatus)
				assert.Equal(t, pr.DueAt, repo.savedItems[0].LmsDueDate)
				adapter := new(mockLmsAdapter)
				want := old
				if !remove {
					want = old.AddDate(0, 0, -2)
					adapter.On("CheckOutItem", "", "added", "", "").Return(&lms.CheckedOutItem{DueDate: &want}, nil).Once()
				}
				shipped := svc.shipLenderRequest(appCtx, "retry", result.pr, adapter, pr.IllRequest, actionParams{})
				require.Equal(t, events.EventStatusSuccess, shipped.status)
				assert.Equal(t, want, shipped.pr.DueAt.Time)
				adapter.AssertExpectations(t)
			})
		}
	}
}
