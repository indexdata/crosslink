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
// may not store. Import records are complete and replace the entry's local
// configuration, so null is always allowed: it clears the stored value. A
// value equal to the one the existing entry already stores is allowed too, so
// re-importing unchanged legacy settings succeeds.
//
// existing is the entry being updated, or nil for a new entry. Call this after
// locking the entry's row, so the stored configuration can't change before the
// import writes.
func (r *PgImportRepo) checkConfigPolicy(ctx context.Context, tx storedconfig.Querier, queries *db.Queries, data model.EntryData, existing *uuid.UUID) error {
	if r.inheritance == nil {
		return nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode entry configuration: %w", err)
	}
	var supplied map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &supplied); err != nil {
		return fmt.Errorf("decode entry configuration: %w", err)
	}
	var stored storedconfig.Configs
	if existing != nil {
		if stored, err = storedconfig.LoadLocal(ctx, tx, *existing); err != nil {
			return err
		}
		if supplied, err = asStored(ctx, queries, supplied, data.ILLConfig); err != nil {
			return err
		}
	}
	violations, err := r.inheritance.CheckWrite(data.Type, supplied, stored)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return errors.New(policy.ViolationMessage(violations))
	}
	return nil
}

// asStored rewrites an import record's configuration containers in the form
// that stored configuration is read back in, so unchanged values compare
// equal: nested nulls are dropped, empty LMS address and agency values are
// unset, and lenders of last resort become authority and symbol objects.
func asStored(ctx context.Context, queries *db.Queries, supplied map[string]json.RawMessage, ill *model.ILLConfig) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(supplied))
	for name, raw := range supplied {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode %s: %w", name, err)
		}
		value = dropNulls(value)
		if container, ok := value.(map[string]any); ok {
			switch name {
			case "lmsConfig":
				for _, field := range []string{"address", "fromAgency"} {
					if container[field] == "" {
						delete(container, field)
					}
				}
			case "illConfig":
				if ill != nil && ill.LendersOfLastResort != nil {
					lenders := make([]any, 0, len(ill.LendersOfLastResort))
					for _, id := range ill.LendersOfLastResort {
						symbol, err := lenderSymbol(ctx, queries, id)
						if err != nil {
							return nil, err
						}
						authority, code, _ := strings.Cut(symbol, ":")
						lenders = append(lenders, map[string]any{"authority": authority, "symbol": code})
					}
					container["lendersOfLastResort"] = lenders
				}
			}
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", name, err)
		}
		result[name] = encoded
	}
	return result, nil
}

// dropNulls removes null members from objects at every depth, matching the
// json_strip_nulls form of stored configuration. A top-level null is kept.
func dropNulls(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, member := range typed {
			if member == nil {
				delete(typed, key)
			} else {
				typed[key] = dropNulls(member)
			}
		}
	case []any:
		for i, member := range typed {
			typed[i] = dropNulls(member)
		}
	}
	return value
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
