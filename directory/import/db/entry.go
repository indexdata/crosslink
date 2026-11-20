package importdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/domain"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	maxImportEntryLockAttempts = 5
	importLockRetryBaseWait    = 10 * time.Millisecond
)

const entrySymbolUniqueConstraint = "symbols_authority_symbol_key"

var errImportEntryMappingChanged = errors.New("entry symbol mapping changed while acquiring import locks")

func (r *PgImportRepo) ImportEntry(ctx context.Context, aggregate model.EntryAggregate, policy model.ConflictPolicy) (model.RepoResult, error) {
	if err := aggregate.NormalizeAndValidate(); err != nil {
		return model.RepoResult{}, err
	}
	return runImportEntryAttempts(ctx, aggregate.Key.String(), func() (model.RepoResult, error) {
		return r.importEntryAttempt(ctx, aggregate, policy)
	})
}

func runImportEntryAttempts(ctx context.Context, key string, attempt func() (model.RepoResult, error)) (model.RepoResult, error) {
	var lastErr error
	for attemptIndex := 0; attemptIndex < maxImportEntryLockAttempts; attemptIndex++ {
		if err := ctx.Err(); err != nil {
			return model.RepoResult{}, err
		}
		result, err := attempt()
		if !retryableImportError(err) {
			return result, err
		}
		lastErr = err
		if err := ctx.Err(); err != nil {
			return model.RepoResult{}, err
		}
		if attemptIndex+1 < maxImportEntryLockAttempts {
			if err := waitForImportRetry(ctx, attemptIndex); err != nil {
				return model.RepoResult{}, err
			}
		}
	}
	return model.RepoResult{}, fmt.Errorf("import entry %s: transaction conflicted repeatedly: %w", key, lastErr)
}

func retryableImportError(err error) bool {
	if errors.Is(err, errImportEntryMappingChanged) {
		return true
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40P01" ||
		pgErr.Code == "40001" ||
		pgErr.Code == "55P03" ||
		(pgErr.Code == "23505" && pgErr.ConstraintName == entrySymbolUniqueConstraint)
}

func waitForImportRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(importLockRetryBaseWait << attempt)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *PgImportRepo) importEntryAttempt(ctx context.Context, aggregate model.EntryAggregate, policy model.ConflictPolicy) (model.RepoResult, error) {
	key := aggregate.Key.String()
	tx, queries, err := r.begin(ctx)
	if err != nil {
		return model.RepoResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := queries.LockEntryImportID(ctx, aggregate.Key); err != nil {
		return model.RepoResult{}, fmt.Errorf("lock entry %s import key: %w", key, err)
	}
	if err := lockEntryImportKeys(ctx, queries, aggregate.Data.Symbols); err != nil {
		return model.RepoResult{}, fmt.Errorf("lock entry %s symbols: %w", key, err)
	}
	existing, lookupErr := queries.EntryByIdForImportUpdate(ctx, aggregate.Key)
	exists := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		return model.RepoResult{}, fmt.Errorf("resolve entry %s: %w", key, lookupErr)
	}
	if exists && policy != model.ConflictPolicyUpdate {
		return conflictResult("entry", key, policy)
	}
	if !exists && policy != model.ConflictPolicyFail && policy != model.ConflictPolicySkip && policy != model.ConflictPolicyUpdate {
		return model.RepoResult{}, fmt.Errorf("invalid conflict policy")
	}

	parent, err := resolveParent(ctx, queries, aggregate.Data.Parent)
	if err != nil {
		return model.RepoResult{}, err
	}
	lenders, err := resolveLenders(ctx, queries, aggregate.Data)
	if err != nil {
		return model.RepoResult{}, err
	}
	var owner *db.Entry
	if exists {
		owner = &existing
	}
	entryIDs := entryLockIDs(owner, parent, lenders)
	lockedEntries, err := lockEntryRowsWithoutWaiting(ctx, queries, entryIDs...)
	if err != nil {
		return model.RepoResult{}, fmt.Errorf("lock entry hierarchy: %w", err)
	}
	if exists {
		existing = lockedEntries[existing.ID]
	}
	if err := validateImportSymbolOwnership(ctx, queries, aggregate.Data.Symbols, aggregate.Key); err != nil {
		return model.RepoResult{}, err
	}
	var parentID *uuid.UUID
	if parent != nil {
		lockedParent := lockedEntries[parent.ID]
		if valid, reason := domain.ValidParentForType(aggregate.Data.Type, lockedParent.Type); !valid {
			return model.RepoResult{}, fmt.Errorf("invalid parent %s: %s", aggregate.Data.Parent, reason)
		}
		parentID = &lockedParent.ID
	}

	if aggregate.Data.Type == "Consortium" || (exists && existing.Type == "Consortium") {
		if err := queries.LockConsortiumEntryChanges(ctx); err != nil {
			return model.RepoResult{}, fmt.Errorf("lock consortium entry changes: %w", err)
		}
	}
	if aggregate.Data.Type == "Consortium" && (!exists || existing.Type != "Consortium") {
		consortium, err := queries.GetConsortialEntry(ctx)
		if err == nil && (!exists || consortium.ID != existing.ID) {
			return model.RepoResult{}, fmt.Errorf("consortium already exists")
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return model.RepoResult{}, fmt.Errorf("check existing consortium: %w", err)
		}
	}

	if exists {
		if err := validateEntryUpdateHierarchy(ctx, queries, existing, aggregate.Data.Type, parentID); err != nil {
			return model.RepoResult{}, err
		}
	}

	entryID, err := writeEntry(ctx, queries, existing, exists, aggregate.Key, parentID, aggregate.Data)
	if err != nil {
		return model.RepoResult{}, persistenceError("entry", key, err)
	}
	if err := replaceEntryChildren(ctx, queries, entryID, aggregate.Data); err != nil {
		return model.RepoResult{}, persistenceError("entry", key, err)
	}
	// Resolve lenders after replacing symbols so an entry that lists itself as a
	// lender uses its newly imported symbol.
	containers, err := storedContainers(ctx, queries, aggregate.Data)
	if err != nil {
		return model.RepoResult{}, err
	}
	// The row lock keeps existing.Configs current from the policy check through
	// the update. For a new entry, existing.Configs is nil.
	if err := r.checkConfigPolicy(aggregate.Data.Type, containers, existing.Configs); err != nil {
		return model.RepoResult{}, err
	}
	if err := queries.UpdateEntryConfigs(ctx, db.UpdateEntryConfigsParams{ID: entryID, Configs: containers}); err != nil {
		return model.RepoResult{}, persistenceError("entry", key, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.RepoResult{}, fmt.Errorf("commit entry %s import: %w", key, err)
	}
	return model.RepoResult{Outcome: model.OutcomeImported}, nil
}

func lockEntryImportKeys(ctx context.Context, queries *db.Queries, refs []model.SymbolRef) error {
	ordered := append([]model.SymbolRef(nil), refs...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Authority != ordered[j].Authority {
			return ordered[i].Authority < ordered[j].Authority
		}
		return ordered[i].Symbol < ordered[j].Symbol
	})
	for index, ref := range ordered {
		if index > 0 && ref == ordered[index-1] {
			continue
		}
		if err := queries.LockEntryImportKey(ctx, db.LockEntryImportKeyParams{Authority: ref.Authority, Symbol: ref.Symbol}); err != nil {
			return err
		}
	}
	return nil
}

func validateImportSymbolOwnership(ctx context.Context, queries *db.Queries, refs []model.SymbolRef, entryID uuid.UUID) error {
	for _, ref := range refs {
		symbol, err := queries.SymbolByAuthorityAndSymbolForUpdate(ctx, db.SymbolByAuthorityAndSymbolForUpdateParams{
			Authority: ref.Authority,
			Symbol:    ref.Symbol,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("resolve entry symbol %s: %w", ref.String(), err)
		}
		if symbol.Owner != entryID {
			return fmt.Errorf("entry symbol %s already belongs to another entry", ref.String())
		}
	}
	return nil
}

func resolveParent(ctx context.Context, queries *db.Queries, parent *uuid.UUID) (*db.Entry, error) {
	if parent == nil {
		return nil, nil
	}
	entry, err := queries.EntryById(ctx, *parent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("parent %s does not exist", parent)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve parent %s: %w", parent, err)
	}
	return &entry, nil
}

func resolveLenders(ctx context.Context, queries *db.Queries, data model.EntryData) ([]db.Entry, error) {
	ids, err := data.LendersOfLastResort()
	if err != nil {
		return nil, err
	}
	lenders := make([]db.Entry, 0, len(ids))
	for _, lender := range ids {
		entry, err := queries.EntryById(ctx, lender)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("lender of last resort %s does not exist", lender)
		}
		if err != nil {
			return nil, fmt.Errorf("resolve lender of last resort %s: %w", lender, err)
		}
		lenders = append(lenders, entry)
	}
	return lenders, nil
}

func entryLockIDs(owner, parent *db.Entry, lenders []db.Entry) []uuid.UUID {
	ids := make([]uuid.UUID, 0, 2+len(lenders))
	if owner != nil {
		ids = append(ids, owner.ID)
	}
	if parent != nil {
		ids = append(ids, parent.ID)
	}
	for _, lender := range lenders {
		ids = append(ids, lender.ID)
	}
	return orderedUniqueEntryIDs(ids...)
}

func lockEntryRowsWithoutWaiting(ctx context.Context, queries *db.Queries, ids ...uuid.UUID) (map[uuid.UUID]db.Entry, error) {
	entries := make(map[uuid.UUID]db.Entry, len(ids))
	for _, id := range orderedUniqueEntryIDs(ids...) {
		entry, err := queries.EntryByIdForImportUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errImportEntryMappingChanged
		}
		if err != nil {
			return nil, err
		}
		entries[id] = entry
	}
	return entries, nil
}

func orderedUniqueEntryIDs(ids ...uuid.UUID) []uuid.UUID {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	ordered := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, exists := unique[id]; exists {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return bytes.Compare(ordered[i][:], ordered[j][:]) < 0
	})
	return ordered
}

func validateEntryUpdateHierarchy(ctx context.Context, queries *db.Queries, existing db.Entry, resultingType string, parentID *uuid.UUID) error {
	if parentID != nil {
		cycle, err := queries.WouldCreateEntryCycle(ctx, db.WouldCreateEntryCycleParams{Child: existing.ID, Parent: *parentID})
		if err != nil {
			return fmt.Errorf("validate entry hierarchy: %w", err)
		}
		if cycle != nil && *cycle {
			return fmt.Errorf("entry parent would create a cycle")
		}
	}
	if resultingType != existing.Type {
		children, err := queries.EntriesByParent(ctx, &existing.ID)
		if err != nil {
			return fmt.Errorf("validate entry children: %w", err)
		}
		for _, child := range children {
			if valid, reason := domain.ValidParentForType(child.Type, resultingType); !valid {
				return fmt.Errorf("entry type is invalid for existing child: %s", reason)
			}
		}
	}
	return nil
}

func writeEntry(ctx context.Context, queries *db.Queries, existing db.Entry, exists bool, id uuid.UUID, parentID *uuid.UUID, data model.EntryData) (uuid.UUID, error) {
	if exists {
		err := queries.UpdateEntry(ctx, db.UpdateEntryParams{
			Name: data.Name, Description: data.Description, ContactName: data.ContactName, Email: data.Email, FromEmail: data.FromEmail,
			Tenant: data.Tenant, Vendor: data.Vendor, PhoneNumber: data.PhoneNumber, TimeZone: data.TimeZone,
			OrganizationID: data.OrganizationID, Type: data.Type, Parent: parentID, LmsLocationCode: data.LMSLocationCode,
			LendToBorrowRatio: data.LendToBorrowRatio, Hrid: data.HRID, ID: existing.ID,
		})
		return existing.ID, err
	}
	created, err := queries.CreateImportedEntry(ctx, db.CreateImportedEntryParams{
		ID:   id,
		Name: data.Name, Description: data.Description, ContactName: data.ContactName, Email: data.Email, FromEmail: data.FromEmail,
		Tenant: data.Tenant, Vendor: data.Vendor, PhoneNumber: data.PhoneNumber, TimeZone: data.TimeZone,
		OrganizationID: data.OrganizationID, Type: data.Type, Parent: parentID, LmsLocationCode: data.LMSLocationCode,
		LendToBorrowRatio: data.LendToBorrowRatio, Hrid: data.HRID,
	})
	return created.ID, err
}

// replaceEntryChildren replaces the symbols, endpoints, addresses and closures
// the entry owns with the import record's values.
func replaceEntryChildren(ctx context.Context, queries *db.Queries, entryID uuid.UUID, data model.EntryData) error {
	if err := queries.DeleteAllOwnedSymbols(ctx, entryID); err != nil {
		return err
	}
	for _, symbol := range data.Symbols {
		if _, err := queries.CreateSymbol(ctx, db.CreateSymbolParams{Owner: entryID, Authority: symbol.Authority, Symbol: symbol.Symbol}); err != nil {
			return err
		}
	}
	if err := queries.DeleteAllOwnedServiceEndpoints(ctx, entryID); err != nil {
		return err
	}
	for _, endpoint := range data.Endpoints {
		if _, err := queries.UpsertServiceEndpoint(ctx, db.UpsertServiceEndpointParams{Entry: entryID, Name: endpoint.Name, Type: endpoint.Type, Address: endpoint.Address}); err != nil {
			return err
		}
	}
	if err := queries.DeleteAllOwnedAddresses(ctx, entryID); err != nil {
		return err
	}
	for _, address := range data.Addresses {
		created, err := queries.UpsertAddress(ctx, db.UpsertAddressParams{Entry: entryID, Type: address.Type})
		if err != nil {
			return err
		}
		for _, component := range address.Components {
			if _, err := queries.CreateAddressComponent(ctx, db.CreateAddressComponentParams{Address: created.ID, Seq: component.Seq, Type: component.Type, Value: component.Value}); err != nil {
				return err
			}
		}
	}
	if err := queries.DeleteClosuresByEntry(ctx, entryID); err != nil {
		return err
	}
	for index, closure := range data.Closures {
		start, err := time.Parse(time.DateOnly, closure.StartDate)
		if err != nil {
			return fmt.Errorf("parse closure %d startDate: %w", index+1, err)
		}
		end, err := time.Parse(time.DateOnly, closure.EndDate)
		if err != nil {
			return fmt.Errorf("parse closure %d endDate: %w", index+1, err)
		}
		if _, err := queries.CreateClosure(ctx, db.CreateClosureParams{
			Entry: entryID, StartDate: pgtype.Timestamp{Time: start, Valid: true}, EndDate: pgtype.Timestamp{Time: end, Valid: true}, Reason: closure.Reason,
		}); err != nil {
			return err
		}
	}
	return nil
}
