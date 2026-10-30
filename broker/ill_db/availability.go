package ill_db

import (
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AvailabilityTokenKey identifies an attempt in completion events and follow-up tasks.
const AvailabilityTokenKey = "availabilityToken"

// AvailabilitySupplierStateKey records the state required by a follow-up task.
const AvailabilitySupplierStateKey = "availabilitySupplierState"

// SaveAvailabilityAttempt replaces coordination state while the caller holds the ILL transaction lock.
func (r *PgIllRepo) SaveAvailabilityAttempt(ctx common.ExtendedContext, p SaveAvailabilityAttemptParams) error {
	return r.queries.SaveAvailabilityAttempt(ctx, r.GetConnOrTx(), p)
}

// GetAvailabilityAttempt reads the most recently started attempt.
func (r *PgIllRepo) GetAvailabilityAttempt(ctx common.ExtendedContext, id string) (AvailabilityAttempt, error) {
	return r.queries.GetAvailabilityAttempt(ctx, r.GetConnOrTx(), id)
}

// DeleteAvailabilityAttempt invalidates outstanding results during supplier reselection.
func (r *PgIllRepo) DeleteAvailabilityAttempt(ctx common.ExtendedContext, id string) error {
	return r.queries.DeleteAvailabilityAttempt(ctx, r.GetConnOrTx(), id)
}

// AvailabilityCurrent checks a tagged follow-up against persisted coordination
// state. The caller must hold the transaction row lock when acting on the result.
// Untagged tasks belong to other workflows or predate attempt tracking.
func AvailabilityCurrent(ctx common.ExtendedContext, repo IllRepo, transactionID string, data map[string]any) (bool, error) {
	raw, tagged := data[AvailabilityTokenKey]
	if !tagged {
		return true, nil
	}
	token, ok := raw.(string)
	state, stateOK := data[AvailabilitySupplierStateKey].(string)
	if !ok || token == "" || !stateOK || (state != SupplierStateSelectedPg.String && state != SupplierStateSkippedPg.String) {
		return false, fmt.Errorf("invalid availability attempt reference")
	}
	attempt, err := repo.GetAvailabilityAttempt(ctx, transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read availability attempt: %w", err)
	}
	if attempt.Token != token {
		return false, nil
	}
	supplier, err := repo.GetLocatedSupplierByIdForUpdate(ctx, attempt.LocatedSupplierID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read availability supplier: %w", err)
	}
	if supplier.IllTransactionID != transactionID || supplier.SupplierStatus != (pgtype.Text{String: state, Valid: true}) {
		return false, nil
	}
	if state == SupplierStateSkippedPg.String {
		// A delayed unavailable result must not advance a rota selected by another path.
		_, err := repo.GetSelectedSupplierForIllTransaction(ctx, transactionID)
		if err == nil {
			return false, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
	}
	return true, nil
}

// CheckAvailabilityCurrent releases its short transaction before catalog or message I/O.
func CheckAvailabilityCurrent(ctx common.ExtendedContext, repo IllRepo, transactionID string, data map[string]any) (bool, error) {
	if _, tagged := data[AvailabilityTokenKey]; !tagged {
		return true, nil
	}
	current := false
	err := repo.WithTxFunc(ctx, func(tx IllRepo) error {
		if _, err := tx.GetIllTransactionByIdForUpdate(ctx, transactionID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		var err error
		current, err = AvailabilityCurrent(ctx, tx, transactionID, data)
		return err
	})
	return current, err
}
