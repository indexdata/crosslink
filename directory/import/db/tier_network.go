package importdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	tierBusinessKeyUniqueConstraint    = "tiers_consortium_name_unique"
	networkBusinessKeyUniqueConstraint = "networks_consortium_name_unique"
)

func (r *PgImportRepo) ImportTier(ctx context.Context, aggregate model.TierAggregate, policy model.ConflictPolicy) (model.RepoResult, error) {
	if err := aggregate.NormalizeAndValidate(); err != nil {
		return model.RepoResult{}, err
	}
	key := aggregate.Key.Consortium.String() + "/" + aggregate.Key.Name
	var lastErr error
	for attemptIndex := 0; attemptIndex < maxImportEntryLockAttempts; attemptIndex++ {
		result, err := r.importTierAttempt(ctx, aggregate, policy, key)
		if !retryableAssignmentImportError(err) {
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
	return model.RepoResult{}, fmt.Errorf("import tier %s: transaction conflicted repeatedly: %w", key, lastErr)
}

func (r *PgImportRepo) importTierAttempt(ctx context.Context, aggregate model.TierAggregate, policy model.ConflictPolicy, key string) (model.RepoResult, error) {
	tx, queries, err := r.begin(ctx)
	if err != nil {
		return model.RepoResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	consortium, assignments, err := resolveAndLockAssignments(ctx, queries, aggregate.Key.Consortium, aggregate.Data.Entries)
	if err != nil {
		return model.RepoResult{}, err
	}
	name := aggregate.Key.Name
	existing, lookupErr := queries.LockTierByBusinessKey(ctx, db.LockTierByBusinessKeyParams{Consortium: consortium.ID, Name: name})
	exists := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		return model.RepoResult{}, fmt.Errorf("resolve tier %s: %w", key, lookupErr)
	}
	if exists && policy != model.ConflictPolicyUpdate {
		return conflictResult("tier", key, policy)
	}
	if !exists && !validPolicy(policy) {
		return model.RepoResult{}, fmt.Errorf("invalid conflict policy")
	}
	assignmentEntries, err := requireAssignmentEntries(assignments)
	if err != nil {
		return model.RepoResult{}, err
	}

	var tierID uuid.UUID
	if exists {
		tierID = existing.ID
		err = queries.UpdateImportedTier(ctx, db.UpdateImportedTierParams{ID: tierID, Level: aggregate.Data.Level, Type: aggregate.Data.Type, Cost: aggregate.Data.Cost})
	} else {
		var created db.Tier
		created, err = queries.CreateTier(ctx, db.CreateTierParams{Name: name, Consortium: consortium.ID, Level: aggregate.Data.Level, Type: aggregate.Data.Type, Cost: aggregate.Data.Cost})
		tierID = created.ID
	}
	if err != nil {
		return model.RepoResult{}, persistenceError("tier", key, err)
	}
	if err := replaceTierAssignments(ctx, queries, tierID, assignmentEntries); err != nil {
		return model.RepoResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.RepoResult{}, fmt.Errorf("commit tier %s import: %w", key, err)
	}
	return model.RepoResult{Outcome: model.OutcomeImported}, nil
}

func (r *PgImportRepo) ImportNetwork(ctx context.Context, aggregate model.NetworkAggregate, policy model.ConflictPolicy) (model.RepoResult, error) {
	if err := aggregate.NormalizeAndValidate(); err != nil {
		return model.RepoResult{}, err
	}
	key := aggregate.Key.Consortium.String() + "/" + aggregate.Key.Name
	var lastErr error
	for attemptIndex := 0; attemptIndex < maxImportEntryLockAttempts; attemptIndex++ {
		result, err := r.importNetworkAttempt(ctx, aggregate, policy, key)
		if !retryableAssignmentImportError(err) {
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
	return model.RepoResult{}, fmt.Errorf("import network %s: transaction conflicted repeatedly: %w", key, lastErr)
}

func (r *PgImportRepo) importNetworkAttempt(ctx context.Context, aggregate model.NetworkAggregate, policy model.ConflictPolicy, key string) (model.RepoResult, error) {
	tx, queries, err := r.begin(ctx)
	if err != nil {
		return model.RepoResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	refs := make([]model.SymbolRef, len(aggregate.Data.Entries))
	for index, assignment := range aggregate.Data.Entries {
		refs[index] = assignment.SymbolRef
	}
	consortium, assignments, err := resolveAndLockAssignments(ctx, queries, aggregate.Key.Consortium, refs)
	if err != nil {
		return model.RepoResult{}, err
	}
	name := aggregate.Key.Name
	existing, lookupErr := queries.LockNetworkByBusinessKey(ctx, db.LockNetworkByBusinessKeyParams{Consortium: consortium.ID, Name: name})
	exists := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		return model.RepoResult{}, fmt.Errorf("resolve network %s: %w", key, lookupErr)
	}
	if exists && policy != model.ConflictPolicyUpdate {
		return conflictResult("network", key, policy)
	}
	if !exists && !validPolicy(policy) {
		return model.RepoResult{}, fmt.Errorf("invalid conflict policy")
	}
	assignmentEntries, err := requireAssignmentEntries(assignments)
	if err != nil {
		return model.RepoResult{}, err
	}

	var networkID uuid.UUID
	if exists {
		networkID = existing.ID
		err = queries.UpdateImportedNetwork(ctx, db.UpdateImportedNetworkParams{ID: networkID, Reciprocal: aggregate.Data.Reciprocal})
	} else {
		var created db.Network
		created, err = queries.CreateNetwork(ctx, db.CreateNetworkParams{Name: name, Consortium: consortium.ID, Reciprocal: aggregate.Data.Reciprocal})
		networkID = created.ID
	}
	if err != nil {
		return model.RepoResult{}, persistenceError("network", key, err)
	}
	if err := replaceNetworkAssignments(ctx, queries, networkID, assignmentEntries, aggregate.Data.Entries); err != nil {
		return model.RepoResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.RepoResult{}, fmt.Errorf("commit network %s import: %w", key, err)
	}
	return model.RepoResult{Outcome: model.OutcomeImported}, nil
}

type resolvedAssignment struct {
	ref   model.SymbolRef
	entry *db.Entry
}

func resolveAndLockAssignments(ctx context.Context, queries *db.Queries, consortiumRef model.SymbolRef, refs []model.SymbolRef) (db.Entry, []resolvedAssignment, error) {
	consortium, err := queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: consortiumRef.Authority, Symbol: consortiumRef.Symbol})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Entry{}, nil, fmt.Errorf("consortium %s does not exist", consortiumRef.String())
	}
	if err != nil {
		return db.Entry{}, nil, fmt.Errorf("resolve consortium %s: %w", consortiumRef.String(), err)
	}

	assignments := make([]resolvedAssignment, 0, len(refs))
	entryIDs := []uuid.UUID{consortium.ID}
	mappings := []entryMapping{{ref: consortiumRef, expectedOwner: &consortium.ID}}
	for _, ref := range refs {
		entry, err := queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: ref.Authority, Symbol: ref.Symbol})
		if errors.Is(err, pgx.ErrNoRows) {
			assignments = append(assignments, resolvedAssignment{ref: ref})
			mappings = append(mappings, entryMapping{ref: ref})
			continue
		}
		if err != nil {
			return db.Entry{}, nil, fmt.Errorf("resolve entry %s: %w", ref.String(), err)
		}
		assignments = append(assignments, resolvedAssignment{ref: ref, entry: &entry})
		entryIDs = append(entryIDs, entry.ID)
		mappings = append(mappings, entryMapping{ref: ref, expectedOwner: &entry.ID})
	}

	lockedEntries, err := lockAssignmentEntryRows(ctx, queries, entryIDs...)
	if err != nil {
		return db.Entry{}, nil, fmt.Errorf("lock assignment entries: %w", err)
	}
	if err := lockEntryMappings(ctx, queries, mappings...); err != nil {
		return db.Entry{}, nil, fmt.Errorf("revalidate assignment entries: %w", err)
	}
	consortium = lockedEntries[consortium.ID]
	if consortium.Type != "Consortium" {
		return db.Entry{}, nil, fmt.Errorf("entry %s is not a consortium", consortiumRef.String())
	}
	for index := range assignments {
		if assignments[index].entry != nil {
			entry := lockedEntries[assignments[index].entry.ID]
			assignments[index].entry = &entry
		}
	}
	return consortium, assignments, nil
}

func retryableAssignmentImportError(err error) bool {
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
		(pgErr.Code == "23505" &&
			(pgErr.ConstraintName == tierBusinessKeyUniqueConstraint ||
				pgErr.ConstraintName == networkBusinessKeyUniqueConstraint))
}

func lockAssignmentEntryRows(ctx context.Context, queries *db.Queries, ids ...uuid.UUID) (map[uuid.UUID]db.Entry, error) {
	ordered := orderedUniqueEntryIDs(ids...)
	entries := make(map[uuid.UUID]db.Entry, len(ordered))
	for index, id := range ordered {
		var entry db.Entry
		var err error
		if index == 0 {
			entry, err = queries.EntryByIdForUpdate(ctx, id)
		} else {
			entry, err = queries.EntryByIdForImportUpdate(ctx, id)
		}
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

func requireAssignmentEntries(assignments []resolvedAssignment) ([]db.Entry, error) {
	entries := make([]db.Entry, 0, len(assignments))
	seen := make(map[uuid.UUID]model.SymbolRef, len(assignments))
	for _, assignment := range assignments {
		if assignment.entry == nil {
			return nil, fmt.Errorf("entry %s does not exist", assignment.ref.String())
		}
		if previous, exists := seen[assignment.entry.ID]; exists {
			return nil, fmt.Errorf("duplicate assignment: symbols %s and %s identify the same entry", previous.String(), assignment.ref.String())
		}
		seen[assignment.entry.ID] = assignment.ref
		entries = append(entries, *assignment.entry)
	}
	return entries, nil
}

func replaceTierAssignments(ctx context.Context, queries *db.Queries, tierID uuid.UUID, entries []db.Entry) error {
	if err := queries.DeleteEntryTiersByTier(ctx, tierID); err != nil {
		return fmt.Errorf("replace tier assignments: delete existing assignments: %w", err)
	}
	for _, entry := range entries {
		if _, err := queries.CreateEntryTier(ctx, db.CreateEntryTierParams{Entry: entry.ID, Tier: tierID}); err != nil {
			return fmt.Errorf("replace tier assignments: create assignment: %w", err)
		}
	}
	return nil
}

func replaceNetworkAssignments(ctx context.Context, queries *db.Queries, networkID uuid.UUID, entries []db.Entry, assignments []model.NetworkAssignment) error {
	if len(entries) != len(assignments) {
		return fmt.Errorf("replace network assignments: entry count mismatch")
	}
	if err := queries.DeleteEntryNetworksByNetwork(ctx, networkID); err != nil {
		return fmt.Errorf("replace network assignments: delete existing assignments: %w", err)
	}
	for index, entry := range entries {
		if _, err := queries.CreateEntryNetwork(ctx, db.CreateEntryNetworkParams{Entry: entry.ID, Network: networkID, Priority: assignments[index].Priority}); err != nil {
			return fmt.Errorf("replace network assignments: create assignment: %w", err)
		}
	}
	return nil
}

func validPolicy(policy model.ConflictPolicy) bool {
	return policy == model.ConflictPolicyFail || policy == model.ConflictPolicySkip || policy == model.ConflictPolicyUpdate
}
