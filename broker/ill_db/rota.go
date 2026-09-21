package ill_db

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	prdb "github.com/indexdata/crosslink/broker/patron_request/db"
)

// SaveRotaAudit persists and notifies observers in the same transaction as the rota edit.
func (r *PgIllRepo) SaveRotaAudit(ctx common.ExtendedContext, transactionID, name, user string, data map[string]any) error {
	if r.Tx == nil {
		return errors.New("rota audit requires a transaction")
	}
	id := uuid.NewString()
	payload, err := json.Marshal(map[string]any{"user": user, "customData": data})
	if err != nil {
		return fmt.Errorf("encode rota audit: %w", err)
	}
	err = r.queries.SaveRotaAudit(ctx, r.GetConnOrTx(), SaveRotaAuditParams{ID: id, IllTransactionID: transactionID, EventName: name, EventData: payload})
	if err != nil {
		return fmt.Errorf("save rota audit: %w", err)
	}
	notice, err := json.Marshal(map[string]string{"event": id, "signal": "notice_created", "target": "observers"})
	if err != nil {
		return err
	}
	return r.queries.NotifyRotaAudit(ctx, r.GetConnOrTx(), string(notice))
}

// RotaRequestClosed locks linked borrowing requests and checks their terminal flags.
// Within WithTxFunc these locks remain held until commit/rollback. Call this before
// locking the ILL transaction to follow the patron-request -> ILL lock order.
func (r *PgIllRepo) RotaRequestClosed(ctx common.ExtendedContext, transactionID string) (bool, error) {
	return (&prdb.Queries{}).RotaRequestClosed(ctx, r.GetConnOrTx(), transactionID)
}
