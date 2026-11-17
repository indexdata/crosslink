package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/storedconfig"
)

// writeSection returns a configuration section from a request in stored
// form. On create the request replaces the section; on PATCH it is merged
// into stored, the entry's current section or nil. A non-empty message
// rejects the request.
func writeSection(name string, stored json.RawMessage, create bool, value any) (json.RawMessage, string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("encode %s: %w", name, err)
	}
	if create {
		data, err = storedconfig.Normalize(name, data)
	} else {
		data, err = storedconfig.Merge(name, stored, data)
	}
	if err != nil {
		return nil, "", err
	}
	if err := storedconfig.Check(name, data); err != nil {
		return nil, err.Error(), nil
	}
	return data, "", nil
}

// sectionPatch is one configuration section of a PATCH body.
type sectionPatch struct {
	name      string
	specified bool
	// value is nil when the section is null, which removes it.
	value any
}

func newSectionPatch[T any](name string, patch nullable.Nullable[T]) sectionPatch {
	section := sectionPatch{name: name, specified: patch.IsSpecified()}
	if value, ok := patchValue(patch); ok {
		section.value = *value
	}
	return section
}

// updateEntryConfigs stores an entry's configuration sections. A section
// missing from configs is removed.
func updateEntryConfigs(ctx context.Context, queries *db.Queries, id uuid.UUID, configs storedconfig.Configs) error {
	ill, lms, catalog, holdings := configs.ToColumns()
	return queries.UpdateEntryConfigs(ctx, db.UpdateEntryConfigsParams{
		ID: id, IllConfig: ill, LmsConfig: lms, CatalogConfig: catalog, HoldingsPolicy: holdings,
	})
}

// patchValue returns a specified, non-null patch value.
func patchValue[T any](patch nullable.Nullable[T]) (*T, bool) {
	value, err := patch.Get()
	if err != nil {
		return nil, false
	}
	return &value, true
}
