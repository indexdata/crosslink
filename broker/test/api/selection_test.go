package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// Pause after the candidate list or after the candidate row lock, allowing the
// protocol update and selection to acquire the supplier lock in either order.
type selectionSupplierBarrier struct {
	rotaClosureBarrier
	peerID string
}

func (r selectionSupplierBarrier) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return r.IllRepo.WithTxFunc(ctx, func(tx ill_db.IllRepo) error {
		r.IllRepo, r.inTx = tx, true
		return fn(r)
	})
}

func (r selectionSupplierBarrier) GetLocatedSuppliersByIllTransactionAndStatus(ctx common.ExtendedContext, params ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	rows, err := r.IllRepo.GetLocatedSuppliersByIllTransactionAndStatus(ctx, params)
	if err == nil && r.inTx && r.beforeCheck {
		err = r.pause(ctx)
	}
	return rows, err
}

func (r selectionSupplierBarrier) GetPeerById(ctx common.ExtendedContext, id string) (ill_db.Peer, error) {
	if r.inTx && !r.beforeCheck && id == r.peerID {
		if err := r.pause(ctx); err != nil {
			return ill_db.Peer{}, err
		}
	}
	return r.IllRepo.GetPeerById(ctx, id)
}

func TestSelectionConcurrentProtocolUpdate(t *testing.T) {
	for _, scenario := range []string{"select", "closed", "already skipped", "already selected"} {
		for _, first := range []string{"protocol", "selection"} {
			t.Run(scenario+"/"+first, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				f := newRotaFixture(t, "new", "new")
				f.ctx = common.CreateExtCtxWithArgs(ctx, nil)
				if scenario == "closed" {
					peer, err := illRepo.GetPeerById(f.ctx, f.suppliers[0].SupplierID)
					require.NoError(t, err)
					data := fmt.Sprintf(`{"closures":[{"startDate":%q,"endDate":%q}],"timeZone":"UTC"}`, time.Now().Add(-48*time.Hour).Format(time.DateOnly), time.Now().Add(48*time.Hour).Format(time.DateOnly))
					require.NoError(t, json.Unmarshal([]byte(data), &peer.CustomData))
					_, err = illRepo.SavePeer(f.ctx, ill_db.SavePeerParams(peer))
					require.NoError(t, err)
				}
				selectionResume, protocolResume := make(chan struct{}), make(chan struct{})
				releaseSelection := sync.OnceFunc(func() { close(selectionResume) })
				releaseProtocol := sync.OnceFunc(func() { close(protocolResume) })
				defer releaseSelection()
				defer releaseProtocol()
				barrier := selectionSupplierBarrier{
					rotaClosureBarrier: rotaClosureBarrier{IllRepo: illRepo, beforeCheck: first == "protocol", paused: make(chan int, 1), resume: selectionResume},
					peerID:             f.suppliers[0].SupplierID,
				}
				bus := &rotaSelectionBus{}
				locator := service.CreateSupplierLocator(bus, barrier, f.directory, nil)
				selectionDone := make(chan struct{})
				go func() { locator.SelectSupplier(f.ctx, events.Event{IllTransactionID: f.id}); close(selectionDone) }()
				selectionPID := awaitRotaPID(t, ctx, barrier.paused)
				expected := f.suppliers[0]
				expected.LastStatus = pgtype.Text{String: "Loaned", Valid: true}
				expected.PrevStatus = pgtype.Text{String: "ExpectToSupply", Valid: true}
				expected.LastAction = pgtype.Text{String: "Request", Valid: true}
				expected.SupplierRequestID = pgtype.Text{String: "protocol-request", Valid: true}
				expected.Note = pgtype.Text{String: "protocol note", Valid: true}
				protocolPIDs := make(chan int, 1)
				protocolDone := make(chan error, 1)
				go func() {
					protocolDone <- illRepo.WithTxFunc(f.ctx, func(repo ill_db.IllRepo) error {
						pid := int(repo.(*ill_db.PgIllRepo).Tx.Conn().PgConn().PID())
						if first == "selection" {
							protocolPIDs <- pid
						}
						sup, err := repo.GetLocatedSupplierByIdForUpdate(f.ctx, expected.ID)
						if err != nil {
							return err
						}
						sup.LastStatus, sup.PrevStatus = expected.LastStatus, expected.PrevStatus
						sup.LastAction, sup.SupplierRequestID, sup.Note = expected.LastAction, expected.SupplierRequestID, expected.Note
						switch scenario {
						case "already skipped":
							sup.SupplierStatus = ill_db.SupplierStateSkippedPg
						case "already selected":
							sup.SupplierStatus = ill_db.SupplierStateSelectedPg
						}
						if _, err := repo.SaveLocatedSupplier(f.ctx, ill_db.SaveLocatedSupplierParams(sup)); err != nil {
							return err
						}
						if first == "protocol" {
							protocolPIDs <- pid
							select {
							case <-protocolResume:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						return nil
					})
				}()
				protocolPID := awaitRotaPID(t, ctx, protocolPIDs)
				if first == "protocol" {
					releaseSelection()
					requireRotaBlockedBy(t, ctx, selectionPID, protocolPID)
					releaseProtocol()
				} else {
					requireRotaBlockedBy(t, ctx, protocolPID, selectionPID)
					releaseSelection()
				}
				awaitRota(t, selectionDone)
				select {
				case err := <-protocolDone:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				require.Equal(t, events.EventStatusSuccess, bus.status)
				expected.SupplierStatus = ill_db.SupplierStateSelectedPg
				if scenario == "closed" || scenario == "already skipped" {
					expected.SupplierStatus = ill_db.SupplierStateSkippedPg
				}
				rows := f.rows(t)
				require.Equal(t, expected, rows[0])
				nextStatus := ill_db.SupplierStateNewPg
				if scenario == "closed" || (first == "protocol" && scenario != "select") {
					nextStatus = ill_db.SupplierStateSelectedPg
				}
				require.Equal(t, nextStatus, rows[1].SupplierStatus)
			})
		}
	}
}
