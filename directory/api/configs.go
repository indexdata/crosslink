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

// writeContainer returns the value to store for one configuration container
// of a request. On create that is the request's container in stored form. On
// PATCH it is the request's container merged into stored, which is the
// entry's current container or nil.
//
// A non-empty message means the container is invalid. It is the text for a
// 400 response, and no container is returned with it.
func writeContainer(name string, stored json.RawMessage, create bool, value any) (json.RawMessage, string, error) {
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

// containerPatch is one configuration container of a PATCH body.
type containerPatch struct {
	name      string
	specified bool
	// value is nil when the container is null, which removes it.
	value any
}

func newContainerPatch[T any](name string, patch nullable.Nullable[T]) containerPatch {
	result := containerPatch{name: name, specified: patch.IsSpecified()}
	if value, ok := patchValue(patch); ok {
		result.value = *value
	}
	return result
}

// updateEntryConfigs stores an entry's configuration containers. A container
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
