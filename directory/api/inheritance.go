package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/policy"
	"github.com/indexdata/crosslink/directory/storedconfig"
)

// maxAncestorDepth bounds the parent chain: a branch's grandparent is the
// consortium, the top of the entry hierarchy.
const maxAncestorDepth = 2

// loadAncestors loads the stored configuration of the parents and
// grandparents of entries, with one query per level.
func loadAncestors(ctx context.Context, q storedconfig.Querier, entries []Entry) (map[uuid.UUID]storedconfig.Entry, error) {
	ancestors := map[uuid.UUID]storedconfig.Entry{}
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
		loaded, err := storedconfig.Load(ctx, q, pending)
		if err != nil {
			return nil, err
		}
		pending = nil
		for id, entry := range loaded {
			ancestors[id] = entry
			queue(entry.Parent)
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
func (a ApiImpl) resolveInheritance(ctx context.Context, q storedconfig.Querier, authData *auth.AuthData, entries []Entry, configs []storedconfig.Configs) error {
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
				Type:    ancestor.Type,
				Tenant:  ancestor.Tenant,
				Configs: ancestor.Configs,
				Redact:  !canSeeSensitiveEntry(authData, ancestor.Tenant),
			})
			parent = ancestor.Parent
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
func (a ApiImpl) checkConfigWrite(entryType string, body any, stored storedconfig.Configs) (string, error) {
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

// resolveInheritParam reports whether a GET should return merged values.
func resolveInheritParam(value *bool) bool {
	return value == nil || *value
}
