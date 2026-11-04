package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/policy"
)

// storedConfigs maps a configuration container's API property name, such as
// lmsConfig, to the JSON object an entry stores locally.
type storedConfigs map[string]json.RawMessage

// querier is satisfied by both the connection pool and a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// maxAncestorDepth bounds the parent chain: a branch's grandparent is the
// consortium, the top of the entry hierarchy.
const maxAncestorDepth = 2

const storedConfigsSQL = `SELECT e.id, e.type, e.parent, e.tenant,` + entryConfigColumns + `
	FROM entries e WHERE e.id = ANY($1)`

type storedEntry struct {
	entryType string
	parent    *uuid.UUID
	tenant    *string
	configs   storedConfigs
}

func loadStoredEntries(ctx context.Context, q querier, ids []uuid.UUID) (map[uuid.UUID]storedEntry, error) {
	rows, err := q.Query(ctx, storedConfigsSQL, ids)
	if err != nil {
		return nil, fmt.Errorf("query stored entry configuration: %w", err)
	}
	defer rows.Close()
	result := make(map[uuid.UUID]storedEntry, len(ids))
	for rows.Next() {
		var (
			id                                     uuid.UUID
			entry                                  storedEntry
			illConfig, lmsConfig, catalog, holding []byte
		)
		if err := rows.Scan(&id, &entry.entryType, &entry.parent, &entry.tenant, &illConfig, &lmsConfig, &catalog, &holding); err != nil {
			return nil, fmt.Errorf("scan stored entry configuration: %w", err)
		}
		entry.configs = storedConfigs{}
		for name, value := range map[string][]byte{"illConfig": illConfig, "lmsConfig": lmsConfig, "catalogConfig": catalog, "holdingsPolicy": holding} {
			if value != nil {
				entry.configs[name] = value
			}
		}
		result[id] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored entry configuration: %w", err)
	}
	return result, nil
}

// loadAncestors loads the stored configuration of the parents and
// grandparents of entries, with one query per level.
func loadAncestors(ctx context.Context, q querier, entries []Entry) (map[uuid.UUID]storedEntry, error) {
	ancestors := map[uuid.UUID]storedEntry{}
	var pending []uuid.UUID
	queued := map[uuid.UUID]bool{}
	queue := func(id *uuid.UUID) {
		if id != nil && !queued[*id] {
			queued[*id] = true
			pending = append(pending, *id)
		}
	}
	for i := range entries {
		queue(entries[i].Parent)
	}
	for depth := 0; depth < maxAncestorDepth && len(pending) > 0; depth++ {
		loaded, err := loadStoredEntries(ctx, q, pending)
		if err != nil {
			return nil, err
		}
		pending = nil
		for id, entry := range loaded {
			ancestors[id] = entry
			queue(entry.parent)
		}
	}
	return ancestors, nil
}

// resolveInheritance replaces each entry's configuration containers with
// their merged view. configs holds each entry's stored containers.
//
// A protected value stored on an ancestor is resolved as "" unless the caller
// may see sensitive data on that ancestor. The caller must still sanitize each
// entry for the requested entry's own visibility.
func (a ApiImpl) resolveInheritance(ctx context.Context, q querier, authData *auth.AuthData, entries []Entry, configs []storedConfigs) error {
	if a.inheritance == nil || len(entries) == 0 {
		return nil
	}
	ancestors, err := loadAncestors(ctx, q, entries)
	if err != nil {
		return err
	}
	for i := range entries {
		entry := &entries[i]
		chain := []policy.Level{{Tenant: entry.Tenant, Configs: configs[i]}}
		if entry.Type != nil {
			chain[0].Type = string(*entry.Type)
		}
		for parent := entry.Parent; parent != nil && len(chain) <= maxAncestorDepth; {
			ancestor, ok := ancestors[*parent]
			if !ok {
				break
			}
			chain = append(chain, policy.Level{
				Type:    ancestor.entryType,
				Tenant:  ancestor.tenant,
				Configs: ancestor.configs,
				Redact:  !canSeeSensitiveEntry(authData, ancestor.tenant),
			})
			parent = ancestor.parent
		}
		resolved, err := a.inheritance.Resolve(chain)
		if err != nil {
			return fmt.Errorf("resolve entry %s: %w", entry.Id, err)
		}
		if err := applyConfigs(entry, resolved); err != nil {
			return fmt.Errorf("apply resolved configuration to entry %s: %w", entry.Id, err)
		}
	}
	return nil
}

func applyConfigs(entry *Entry, configs map[string]json.RawMessage) error {
	var err error
	if entry.IllConfig, err = unmarshalJSONObject[IllConfig](configs["illConfig"]); err != nil {
		return fmt.Errorf("decode illConfig: %w", err)
	}
	if entry.LmsConfig, err = unmarshalJSONObject[LmsConfig](configs["lmsConfig"]); err != nil {
		return fmt.Errorf("decode lmsConfig: %w", err)
	}
	if entry.CatalogConfig, err = unmarshalJSONObject[CatalogConfig](configs["catalogConfig"]); err != nil {
		return fmt.Errorf("decode catalogConfig: %w", err)
	}
	if entry.HoldingsPolicy, err = unmarshalJSONObject[HoldingsPolicy](configs["holdingsPolicy"]); err != nil {
		return fmt.Errorf("decode holdingsPolicy: %w", err)
	}
	return nil
}

// checkConfigWrite returns a client-facing message when body sets
// configuration that entryType may not store. stored is the entry's current
// local configuration, or nil for a new entry.
func (a ApiImpl) checkConfigWrite(entryType string, body any, stored storedConfigs) (string, error) {
	if a.inheritance == nil {
		return "", nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode request body: %w", err)
	}
	var supplied map[string]json.RawMessage
	if err := json.Unmarshal(data, &supplied); err != nil {
		return "", fmt.Errorf("decode request body: %w", err)
	}
	violations, err := a.inheritance.CheckWrite(entryType, supplied, stored)
	if err != nil {
		return "", err
	}
	return policy.ViolationMessage(violations), nil
}

// loadLocalConfigs returns an entry's stored configuration containers.
func loadLocalConfigs(ctx context.Context, q querier, id uuid.UUID) (storedConfigs, error) {
	entries, err := loadStoredEntries(ctx, q, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	return entries[id].configs, nil
}

// resolveInheritParam reports whether a GET should return merged values.
func resolveInheritParam(value *bool) bool {
	return value == nil || *value
}
