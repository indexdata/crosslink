// Package storedconfig handles the configuration containers that directory
// entries store locally. A container is stored as the JSON object that entry
// reads return. The package loads containers and puts written values into
// that form.
package storedconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Columns lists an entry's stored ill_config, lms_config, catalog_config and
// holdings_policy, in that order. It expects the entries table aliased as e.
const Columns = `
		e.ill_config, e.lms_config, e.catalog_config, e.holdings_policy`

// Configs maps a configuration container's API property name, such as
// lmsConfig, to the JSON object an entry stores locally.
type Configs map[string]json.RawMessage

// FromColumns builds Configs from the four columns of Columns, omitting
// containers the entry does not store.
func FromColumns(illConfig, lmsConfig, catalogConfig, holdingsPolicy []byte) Configs {
	configs := Configs{}
	for name, value := range map[string][]byte{
		IllConfig: illConfig, LmsConfig: lmsConfig, CatalogConfig: catalogConfig, HoldingsPolicy: holdingsPolicy,
	} {
		if value != nil {
			configs[name] = value
		}
	}
	return configs
}

// ToColumns returns the four columns of Columns for Configs. A missing
// container is nil, which stores NULL.
func (c Configs) ToColumns() (illConfig, lmsConfig, catalogConfig, holdingsPolicy []byte) {
	return c[IllConfig], c[LmsConfig], c[CatalogConfig], c[HoldingsPolicy]
}

// Querier is satisfied by both a connection pool and a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Entry is an entry's place in the hierarchy and its stored configuration.
type Entry struct {
	Type    string
	Parent  *uuid.UUID
	Tenant  *string
	Configs Configs
}

const loadSQL = `SELECT e.id, e.type, e.parent, e.tenant,` + Columns + `
	FROM entries e WHERE e.id = ANY($1)`

// Load returns the stored configuration of the entries with the given IDs.
// Missing entries are absent from the result.
func Load(ctx context.Context, q Querier, ids []uuid.UUID) (map[uuid.UUID]Entry, error) {
	rows, err := q.Query(ctx, loadSQL, ids)
	if err != nil {
		return nil, fmt.Errorf("query stored entry configuration: %w", err)
	}
	defer rows.Close()
	result := make(map[uuid.UUID]Entry, len(ids))
	for rows.Next() {
		var (
			id                                     uuid.UUID
			entry                                  Entry
			illConfig, lmsConfig, catalog, holding []byte
		)
		if err := rows.Scan(&id, &entry.Type, &entry.Parent, &entry.Tenant, &illConfig, &lmsConfig, &catalog, &holding); err != nil {
			return nil, fmt.Errorf("scan stored entry configuration: %w", err)
		}
		entry.Configs = FromColumns(illConfig, lmsConfig, catalog, holding)
		result[id] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored entry configuration: %w", err)
	}
	return result, nil
}

// LoadLocal returns one entry's stored configuration, or nil if the entry
// does not exist.
func LoadLocal(ctx context.Context, q Querier, id uuid.UUID) (Configs, error) {
	entries, err := Load(ctx, q, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	return entries[id].Configs, nil
}
