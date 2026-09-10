package importdb

import (
	"context"
	"errors"
	"fmt"
	"sort"

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
	tx, queries, err := r.begin(ctx)
	if err != nil {
		return model.RepoResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	consortium, err := resolveConsortium(ctx, queries, aggregate.Key.Consortium)
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
	if err := replaceTierAssignments(ctx, queries, tierID, aggregate.Data.Entries); err != nil {
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
	tx, queries, err := r.begin(ctx)
	if err != nil {
		return model.RepoResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	consortium, err := resolveConsortium(ctx, queries, aggregate.Key.Consortium)
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
	if err := replaceNetworkAssignments(ctx, queries, networkID, aggregate.Data.Entries); err != nil {
		return model.RepoResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.RepoResult{}, fmt.Errorf("commit network %s import", key)
	}
	return model.RepoResult{Outcome: model.OutcomeImported}, nil
}

// resolveConsortium locks the consortium entry for the transaction. Besides
// protecting the reference, this serializes missing tier and network business
// keys within a consortium before their lookup-and-create flows.
func resolveConsortium(ctx context.Context, queries *db.Queries, key model.SymbolRef) (db.Entry, error) {
	entry, err := resolveEntry(ctx, queries, key)
	if err != nil {
		return db.Entry{}, fmt.Errorf("consortium %s does not exist", key.String())
	}
	if entry.Type != "Consortium" {
		return db.Entry{}, fmt.Errorf("entry %s is not a consortium", key.String())
	}
	return entry, nil
}

func replaceTierAssignments(ctx context.Context, queries *db.Queries, tierID uuid.UUID, refs []model.SymbolRef) error {
	if err := queries.DeleteEntryTiersByTier(ctx, tierID); err != nil {
		return fmt.Errorf("replace tier assignments")
	}
	for _, ref := range sortedRefs(refs) {
		entry, err := resolveEntry(ctx, queries, ref)
		if err != nil {
			return err
		}
		if _, err := queries.CreateEntryTier(ctx, db.CreateEntryTierParams{Entry: entry.ID, Tier: tierID}); err != nil {
			return fmt.Errorf("replace tier assignments")
		}
	}
	return nil
}

func replaceNetworkAssignments(ctx context.Context, queries *db.Queries, networkID uuid.UUID, refs []model.SymbolRef) error {
	if err := queries.DeleteEntryNetworksByNetwork(ctx, networkID); err != nil {
		return fmt.Errorf("replace network assignments")
	}
	for _, ref := range sortedRefs(refs) {
		entry, err := resolveEntry(ctx, queries, ref)
		if err != nil {
			return err
		}
		if _, err := queries.CreateEntryNetwork(ctx, db.CreateEntryNetworkParams{Entry: entry.ID, Network: networkID}); err != nil {
			return fmt.Errorf("replace network assignments")
		}
	}
	return nil
}

func sortedRefs(refs []model.SymbolRef) []model.SymbolRef {
	result := append([]model.SymbolRef(nil), refs...)
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result
}

func validPolicy(policy model.ConflictPolicy) bool {
	return policy == model.ConflictPolicyFail || policy == model.ConflictPolicySkip || policy == model.ConflictPolicyUpdate
}
