package service

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Rota errors distinguish client conflicts from storage/directory failures.
var (
	ErrRotaNotFound = errors.New("transaction or located supplier not found")
	ErrRotaConflict = errors.New("rota cannot be changed")
	ErrRotaSymbol   = errors.New("unknown or unusable supplier symbol")
)

// RotaService edits the untried portion of a rota, independent of request origin.
type RotaService struct {
	repo      ill_db.IllRepo
	directory adapter.DirectoryLookupAdapter
}

// NewRotaService uses the existing Directory adapter and transaction repository.
func NewRotaService(repo ill_db.IllRepo, directory adapter.DirectoryLookupAdapter) *RotaService {
	return &RotaService{repo: repo, directory: directory}
}

func (s *RotaService) editable(ctx common.ExtendedContext, repo ill_db.IllRepo, id string, owner tenant.Tenant, lock bool) (ill_db.IllTransaction, error) {
	var trans ill_db.IllTransaction
	var err error
	if lock {
		trans, err = repo.GetIllTransactionByIdForUpdate(ctx, id)
	} else {
		trans, err = repo.GetIllTransactionById(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return trans, ErrRotaNotFound
	}
	if err != nil {
		return trans, err
	}
	owned, err := owner.IsOwnerOf(trans.RequesterSymbol.String)
	if err != nil {
		return trans, err
	}
	if !owned {
		return trans, ErrRotaNotFound
	}
	switch iso18626.TypeStatus(trans.LastSupplierStatus.String) {
	case iso18626.TypeStatusLoanCompleted, iso18626.TypeStatusCopyCompleted, iso18626.TypeStatusCompletedWithoutReturn:
		return trans, fmt.Errorf("%w: transaction is completed", ErrRotaConflict)
	}
	closed, err := repo.RotaRequestClosed(ctx, id)
	if err != nil {
		return trans, err
	}
	if closed {
		return trans, fmt.Errorf("%w: borrowing request is completed", ErrRotaConflict)
	}
	return trans, nil
}

// Move changes only new suppliers' ordinal assignments. The parent lock is also
// held by automatic selection; the status is checked after acquiring that lock.
func (s *RotaService) Move(ctx common.ExtendedContext, id, supplierID string, offset int64, owner tenant.Tenant) ([]ill_db.GetLocatedSuppliersWithPeerByIllTransactionRow, error) {
	owner, err := snapshotRotaOwner(owner)
	if err != nil {
		return nil, err
	}
	// Resolve ownership before taking database locks (branch resolution can use Directory).
	if _, err := s.editable(ctx, s.repo, id, owner, false); err != nil {
		return nil, err
	}
	var result []ill_db.GetLocatedSuppliersWithPeerByIllTransactionRow
	err = s.repo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		if _, err := s.editable(ctx, repo, id, owner, true); err != nil {
			return err
		}
		suppliers, _, err := repo.GetLocatedSuppliersByIllTransaction(ctx, id)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(suppliers, func(s ill_db.LocatedSupplier) bool { return s.ID == supplierID })
		if index < 0 {
			return ErrRotaNotFound
		}
		if suppliers[index].SupplierStatus != ill_db.SupplierStateNewPg {
			return fmt.Errorf("%w: supplier is no longer new", ErrRotaConflict)
		}
		newSuppliers := newRotaSuppliers(suppliers)
		from := slices.IndexFunc(newSuppliers, func(s ill_db.LocatedSupplier) bool { return s.ID == supplierID })
		// Clamp before addition to avoid overflowing an extreme offset.
		to := from + int(max(-int64(from), min(offset, int64(len(newSuppliers)-1-from))))
		if from != to {
			if err = moveNewSupplier(ctx, repo, suppliers, newSuppliers, from, to); err != nil {
				return err
			}
			err = repo.SaveRotaAudit(ctx, id, string(events.EventNameSupplierMoved), owner.GetUser(), map[string]any{"supplierId": supplierID, "supplierSymbol": suppliers[index].SupplierSymbol, "offset": offset, "from": from, "to": to})
			if err != nil {
				return err
			}
		}
		result, _, err = repo.GetLocatedSuppliersWithPeerByIllTransaction(ctx, id)
		return err
	})
	return result, err
}

// Add bypasses holdings discovery/ranking, but does not trigger selection or sending.
func (s *RotaService) Add(ctx common.ExtendedContext, id, symbol, localID string, owner tenant.Tenant) (ill_db.GetLocatedSuppliersWithPeerByIllTransactionRow, error) {
	var result ill_db.GetLocatedSuppliersWithPeerByIllTransactionRow
	owner, err := snapshotRotaOwner(owner)
	if err != nil {
		return result, err
	}
	if _, err := s.editable(ctx, s.repo, id, owner, false); err != nil {
		return result, err
	}
	// Give duplicates precedence over Directory changes, then repeat under the lock.
	if _, err := s.repo.GetLocatedSupplierByIllTransactionAndSymbol(ctx, id, symbol); err == nil {
		return result, fmt.Errorf("%w: symbol already in rota", ErrRotaConflict)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	peer, err := s.resolve(ctx, symbol)
	if err != nil {
		return result, err
	}
	err = s.repo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		trans, err := s.editable(ctx, repo, id, owner, true)
		if err != nil {
			return err
		}
		suppliers, _, err := repo.GetLocatedSuppliersByIllTransaction(ctx, id)
		if err != nil {
			return err
		}
		for _, sup := range suppliers {
			if sup.SupplierSymbol == symbol {
				return fmt.Errorf("%w: symbol already in rota", ErrRotaConflict)
			}
		}
		ordinal, err := freeRotaOrdinal(suppliers)
		if err != nil {
			return err
		}
		added, err := repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{
			ID: uuid.NewString(), IllTransactionID: id, SupplierID: peer.ID, SupplierSymbol: symbol, Ordinal: ordinal,
			SupplierStatus: ill_db.SupplierStateNewPg, LocalID: pgtype.Text{String: localID, Valid: true}, LocalSupplier: peer.ID == trans.RequesterID.String,
		})
		if err != nil {
			return err
		}
		// The new slot is after the rota. Rotate new entries through their existing
		// slots, preserving every selected/skipped ordinal and all relative ordering.
		news := append(newRotaSuppliers(suppliers), added)
		if len(news) > 1 {
			if err = moveNewSupplier(ctx, repo, append(suppliers, added), news, len(news)-1, 0); err != nil {
				return err
			}
		}
		if err = repo.SaveRotaAudit(ctx, id, string(events.EventNameSupplierAdded), owner.GetUser(), map[string]any{"supplierId": added.ID, "supplierSymbol": symbol, "localId": localID}); err != nil {
			return err
		}
		rows, _, err := repo.GetLocatedSuppliersWithPeerByIllTransaction(ctx, id)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.LocatedSupplier.ID == added.ID {
				result = row
				return nil
			}
		}
		return errors.New("created supplier missing from rota")
	})
	return result, err
}

func (s *RotaService) resolve(ctx common.ExtendedContext, symbol string) (ill_db.Peer, error) {
	if s.directory == nil {
		return ill_db.Peer{}, errors.New("directory adapter not configured")
	}
	entries, _, err := s.directory.Lookup(ctx, adapter.DirectoryLookupParams{Symbols: []string{symbol}})
	if err != nil {
		return ill_db.Peer{}, fmt.Errorf("resolve supplier: %w", err)
	}
	matches := 0
	for _, entry := range entries {
		if slices.Contains(entry.Symbols, symbol) {
			matches++
		}
	}
	if matches != 1 {
		return ill_db.Peer{}, ErrRotaSymbol
	}
	peers, _, err := s.repo.GetCachedPeersBySymbols(ctx, []string{symbol}, s.directory)
	if err != nil {
		return ill_db.Peer{}, fmt.Errorf("cache supplier: %w", err)
	}
	if len(peers) != 1 {
		return ill_db.Peer{}, ErrRotaSymbol
	}
	peer := peers[0]
	u, err := url.Parse(peer.Url)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return ill_db.Peer{}, ErrRotaSymbol
	}
	return peer, nil
}

func newRotaSuppliers(all []ill_db.LocatedSupplier) []ill_db.LocatedSupplier {
	var result []ill_db.LocatedSupplier
	for _, s := range all {
		if s.SupplierStatus == ill_db.SupplierStateNewPg {
			result = append(result, s)
		}
	}
	return result
}

func freeRotaOrdinal(all []ill_db.LocatedSupplier) (int32, error) {
	var maxOrdinal int32 = -1
	for _, s := range all {
		maxOrdinal = max(maxOrdinal, s.Ordinal)
	}
	if maxOrdinal == math.MaxInt32 {
		return 0, fmt.Errorf("%w: ordinal space exhausted", ErrRotaConflict)
	}
	return maxOrdinal + 1, nil
}

func moveNewSupplier(ctx common.ExtendedContext, repo ill_db.IllRepo, all, news []ill_db.LocatedSupplier, from, to int) error {
	temporary, err := freeRotaOrdinal(all)
	if err != nil {
		return err
	}
	target := news[from]
	target.Ordinal = temporary
	if _, err = repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(target)); err != nil {
		return err
	}
	step := 1
	if to < from {
		step = -1
	}
	for i := from; i != to; i += step {
		supplier := news[i+step]
		supplier.Ordinal = news[i].Ordinal
		if _, err = repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(supplier)); err != nil {
			return err
		}
	}
	target.Ordinal = news[to].Ordinal
	_, err = repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(target))
	return err
}

// Freeze the authorization scope before locking; Directory lookups must never run
// while holding the transaction lock. The requester is checked again under lock.
type rotaOwner struct {
	tenant.Tenant
	symbols []string
}

func (o rotaOwner) IsOwnerOf(symbol string) (bool, error) {
	return o.symbols == nil || slices.Contains(o.symbols, symbol), nil
}
func snapshotRotaOwner(owner tenant.Tenant) (tenant.Tenant, error) {
	symbols, err := owner.GetOwnedSymbols()
	if err != nil {
		return nil, err
	}
	return rotaOwner{Tenant: owner, symbols: symbols}, nil
}
