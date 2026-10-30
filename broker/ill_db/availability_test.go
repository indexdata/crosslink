package ill_db

import (
	"context"
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type attemptGuardRepo struct {
	IllRepo
	attempt            AvailabilityAttempt
	supplier           LocatedSupplier
	readErr, commitErr error
}

func (r *attemptGuardRepo) WithTxFunc(ctx common.ExtendedContext, f func(IllRepo) error) error {
	if err := f(r); err != nil {
		return err
	}
	return r.commitErr
}
func (r *attemptGuardRepo) GetIllTransactionByIdForUpdate(common.ExtendedContext, string) (IllTransaction, error) {
	return IllTransaction{}, nil
}
func (r *attemptGuardRepo) GetAvailabilityAttempt(common.ExtendedContext, string) (AvailabilityAttempt, error) {
	return r.attempt, r.readErr
}
func (r *attemptGuardRepo) GetLocatedSupplierByIdForUpdate(common.ExtendedContext, string) (LocatedSupplier, error) {
	return r.supplier, nil
}

func TestAvailabilityGuardFailsClosed(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	for _, tc := range []struct {
		name               string
		data               map[string]any
		readErr, commitErr error
		want               bool
		wantErr            bool
	}{
		{name: "unrelated workflow", want: true},
		{name: "malformed reference", data: map[string]any{AvailabilityTokenKey: ""}, wantErr: true},
		{name: "missing attempt", readErr: pgx.ErrNoRows},
		{name: "read failure", readErr: errors.New("read failed"), wantErr: true},
		{name: "commit failure", commitErr: errors.New("commit failed"), wantErr: true},
		{name: "current", want: true},
		{name: "old token", data: map[string]any{AvailabilityTokenKey: "old", AvailabilitySupplierStateKey: SupplierStateSelected}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &attemptGuardRepo{attempt: AvailabilityAttempt{Token: "current", LocatedSupplierID: "supplier"}, supplier: LocatedSupplier{IllTransactionID: "ill", SupplierStatus: SupplierStateSelectedPg}, readErr: tc.readErr, commitErr: tc.commitErr}
			data := tc.data
			if data == nil && tc.name != "unrelated workflow" {
				data = map[string]any{AvailabilityTokenKey: "current", AvailabilitySupplierStateKey: SupplierStateSelected}
			}
			current, err := CheckAvailabilityCurrent(ctx, repo, "ill", data)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, current)
			}
		})
	}
}
