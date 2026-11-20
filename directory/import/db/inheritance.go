package importdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/indexdata/crosslink/directory/policy"
	"github.com/indexdata/crosslink/directory/storedconfig"
)

// checkConfigPolicy checks the replacement configuration against the entry
// type's write policy. Because an import replaces the entire local
// configuration, null is always allowed to clear a value. Unchanged legacy
// values are allowed so records with older settings can be re-imported.
//
// For an existing entry, pass the local configuration from the row locked
// for this import. For a new entry, pass nil.
func (r *PgImportRepo) checkConfigPolicy(entryType string, containers, stored storedconfig.Configs) error {
	if r.inheritance == nil {
		return nil
	}
	violations, err := r.inheritance.CheckWrite(entryType, containers, stored)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return errors.New(policy.ViolationMessage(violations))
	}
	return nil
}

// storedContainers normalizes the imported containers for storage, omitting
// null containers and resolving lender entry IDs to directory symbols.
func storedContainers(ctx context.Context, queries *db.Queries, data model.EntryData) (storedconfig.Configs, error) {
	lenders, err := data.LendersOfLastResort()
	if err != nil {
		return nil, err
	}
	containers := storedconfig.Configs{}
	for name, container := range data.Containers() {
		if name == storedconfig.IllConfig && lenders != nil {
			if container, err = withLenderSymbols(ctx, queries, container, lenders); err != nil {
				return nil, err
			}
		}
		normalized, err := storedconfig.Normalize(name, container)
		if err != nil {
			return nil, err
		}
		if string(normalized) != "null" {
			containers[name] = normalized
		}
	}
	return containers, nil
}

// withLenderSymbols replaces each lender ID in illConfig with a JSON object
// containing the lender's authority and symbol.
func withLenderSymbols(ctx context.Context, queries *db.Queries, illConfig json.RawMessage, ids []uuid.UUID) (json.RawMessage, error) {
	var config map[string]any
	if err := json.Unmarshal(illConfig, &config); err != nil {
		return nil, fmt.Errorf("decode illConfig: %w", err)
	}
	lenders := make([]any, 0, len(ids))
	for _, id := range ids {
		symbol, err := lenderSymbol(ctx, queries, id)
		if err != nil {
			return nil, err
		}
		authority, code, _ := strings.Cut(symbol, ":")
		lenders = append(lenders, map[string]any{"authority": authority, "symbol": code})
	}
	config["lendersOfLastResort"] = lenders
	return json.Marshal(config)
}

// lenderSymbol returns the first directory symbol of a lender of last
// resort, formatted as AUTHORITY:SYMBOL.
func lenderSymbol(ctx context.Context, queries *db.Queries, id uuid.UUID) (string, error) {
	symbol, err := queries.FirstSymbolByOwner(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("lender of last resort %s has no symbol", id)
	}
	if err != nil {
		return "", fmt.Errorf("resolve lender of last resort %s symbol: %w", id, err)
	}
	return symbol.Authority + ":" + symbol.Symbol, nil
}
