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

// checkConfigPolicy rejects configuration values that the record's entry type
// may not store. sections are the record's configuration in stored form, as
// storedSections returns it. Import records are complete and replace the
// entry's local configuration, so null is always allowed: it clears the
// stored value. A value equal to the one the existing entry already stores is
// allowed too, so re-importing unchanged legacy settings succeeds.
//
// existing is the entry being updated, or nil for a new entry. Call this after
// locking the entry's row, so the stored configuration can't change before the
// import writes.
func (r *PgImportRepo) checkConfigPolicy(ctx context.Context, tx storedconfig.Querier, entryType string, sections storedconfig.Configs, existing *uuid.UUID) error {
	if r.inheritance == nil {
		return nil
	}
	var stored storedconfig.Configs
	if existing != nil {
		var err error
		if stored, err = storedconfig.LoadLocal(ctx, tx, *existing); err != nil {
			return err
		}
	}
	violations, err := r.inheritance.CheckWrite(entryType, sections, stored)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return errors.New(policy.ViolationMessage(violations))
	}
	return nil
}

// storedSections returns an import record's configuration sections in stored
// form. A null section is absent from the result. Lenders of last resort,
// which the record names by entry ID, become the authority and symbol they
// are stored under.
func storedSections(ctx context.Context, queries *db.Queries, data model.EntryData) (storedconfig.Configs, error) {
	lenders, err := data.LendersOfLastResort()
	if err != nil {
		return nil, err
	}
	sections := storedconfig.Configs{}
	for name, section := range data.Sections() {
		if name == storedconfig.IllConfig && lenders != nil {
			if section, err = withLenderSymbols(ctx, queries, section, lenders); err != nil {
				return nil, err
			}
		}
		normalized, err := storedconfig.Normalize(name, section)
		if err != nil {
			return nil, err
		}
		if string(normalized) != "null" {
			sections[name] = normalized
		}
	}
	return sections, nil
}

// withLenderSymbols replaces the lender entry IDs in an encoded illConfig
// with the authority and symbol objects the lenders are stored under.
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

// lenderSymbol returns the AUTHORITY:SYMBOL under which a lender of last
// resort is stored.
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
