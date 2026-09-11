package importdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/jackc/pgx/v5"
)

func (r *PgImportRepo) ImportTier(ctx context.Context, aggregate model.TierAggregate, policy model.ConflictPolicy) (model.RepoResult, error) {
	if err := aggregate.NormalizeAndValidate(); err != nil {
		return model.RepoResult{}, err
	}
	key := aggregate.Key.Consortium.String() + "/" + aggregate.Key.Name
	for range maxImportLockAttempts {
		result, err := r.importTierAttempt(ctx, aggregate, policy, key)
		if !errors.Is(err, errImportEntryMappingChanged) {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return model.RepoResult{}, err
		}
	}
	return model.RepoResult{}, fmt.Errorf("import tier %s: entry mappings changed repeatedly", key)
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
		return model.RepoResult{}, fmt.Errorf("resolve tier %s", key)
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
		return model.RepoResult{}, fmt.Errorf("commit tier %s import", key)
	}
	return model.RepoResult{Outcome: model.OutcomeImported}, nil
}

func (r *PgImportRepo) ImportNetwork(ctx context.Context, aggregate model.NetworkAggregate, policy model.ConflictPolicy) (model.RepoResult, error) {
	if err := aggregate.NormalizeAndValidate(); err != nil {
		return model.RepoResult{}, err
	}
	key := aggregate.Key.Consortium.String() + "/" + aggregate.Key.Name
	for range maxImportLockAttempts {
		result, err := r.importNetworkAttempt(ctx, aggregate, policy, key)
		if !errors.Is(err, errImportEntryMappingChanged) {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return model.RepoResult{}, err
		}
	}
	return model.RepoResult{}, fmt.Errorf("import network %s: entry mappings changed repeatedly", key)
}

func (r *PgImportRepo) importNetworkAttempt(ctx context.Context, aggregate model.NetworkAggregate, policy model.ConflictPolicy, key string) (model.RepoResult, error) {
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
	existing, lookupErr := queries.LockNetworkByBusinessKey(ctx, db.LockNetworkByBusinessKeyParams{Consortium: consortium.ID, Name: name})
	exists := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		return model.RepoResult{}, fmt.Errorf("resolve network %s", key)
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
		err = queries.UpdateImportedNetwork(ctx, db.UpdateImportedNetworkParams{ID: networkID, Priority: aggregate.Data.Priority, Reciprocal: aggregate.Data.Reciprocal})
	} else {
		var created db.Network
		created, err = queries.CreateNetwork(ctx, db.CreateNetworkParams{Name: name, Consortium: consortium.ID, Priority: aggregate.Data.Priority, Reciprocal: aggregate.Data.Reciprocal})
		networkID = created.ID
	}
	if err != nil {
		return model.RepoResult{}, persistenceError("network", key, err)
	}
	if err := replaceNetworkAssignments(ctx, queries, networkID, assignmentEntries); err != nil {
		return model.RepoResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.RepoResult{}, fmt.Errorf("commit network %s import", key)
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
		return db.Entry{}, nil, fmt.Errorf("resolve consortium %s", consortiumRef.String())
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
			return db.Entry{}, nil, fmt.Errorf("resolve entry %s", ref.String())
		}
		assignments = append(assignments, resolvedAssignment{ref: ref, entry: &entry})
		entryIDs = append(entryIDs, entry.ID)
		mappings = append(mappings, entryMapping{ref: ref, expectedOwner: &entry.ID})
	}

	lockedEntries, err := lockEntryRows(ctx, queries, entryIDs...)
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

func requireAssignmentEntries(assignments []resolvedAssignment) ([]db.Entry, error) {
	entries := make([]db.Entry, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.entry == nil {
			return nil, fmt.Errorf("entry %s does not exist", assignment.ref.String())
		}
		entries = append(entries, *assignment.entry)
	}
	return entries, nil
}

func replaceTierAssignments(ctx context.Context, queries *db.Queries, tierID uuid.UUID, entries []db.Entry) error {
	if err := queries.DeleteEntryTiersByTier(ctx, tierID); err != nil {
		return fmt.Errorf("replace tier assignments")
	}
	for _, entry := range entries {
		if _, err := queries.CreateEntryTier(ctx, db.CreateEntryTierParams{Entry: entry.ID, Tier: tierID}); err != nil {
			return fmt.Errorf("replace tier assignments")
		}
	}
	return nil
}

func replaceNetworkAssignments(ctx context.Context, queries *db.Queries, networkID uuid.UUID, entries []db.Entry) error {
	if err := queries.DeleteEntryNetworksByNetwork(ctx, networkID); err != nil {
		return fmt.Errorf("replace network assignments")
	}
	for _, entry := range entries {
		if _, err := queries.CreateEntryNetwork(ctx, db.CreateEntryNetworkParams{Entry: entry.ID, Network: networkID}); err != nil {
			return fmt.Errorf("replace network assignments")
		}
	}
	return nil
}

func validPolicy(policy model.ConflictPolicy) bool {
	return policy == model.ConflictPolicyFail || policy == model.ConflictPolicySkip || policy == model.ConflictPolicyUpdate
}
