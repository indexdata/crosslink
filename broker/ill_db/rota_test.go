package ill_db

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestRotaAuditRequiresTransactionAndRollsBackOnFailure(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	id := uuid.NewString()
	require.ErrorContains(t, illRepo.SaveRotaAudit(ctx, id, "supplier-added", "staff", nil), "requires a transaction")
	err := illRepo.WithTxFunc(ctx, func(repo IllRepo) error {
		if _, err := repo.SaveIllTransaction(ctx, SaveIllTransactionParams{ID: id, Timestamp: GetPgNow()}); err != nil {
			return err
		}
		// A missing event configuration must fail the entire mutation, not leave
		// data committed without its audit record.
		return repo.SaveRotaAudit(ctx, id, "unregistered-rota-event", "staff", nil)
	})
	require.ErrorContains(t, err, "save rota audit")
	_, err = illRepo.GetIllTransactionById(ctx, id)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
