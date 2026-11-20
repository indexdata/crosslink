package api

import (
	"encoding/json"
	"fmt"

	"github.com/oapi-codegen/nullable"

	"github.com/indexdata/crosslink/directory/storedconfig"
)

// containerNames lists the configuration containers in the order a request's
// containers are written and checked.
var containerNames = []string{
	storedconfig.IllConfig, storedconfig.LmsConfig, storedconfig.CatalogConfig, storedconfig.HoldingsPolicy,
}

// bodyProperties returns the top-level properties of a create or PATCH body
// as JSON. A property the body omits is absent, and one it sets to null is
// the JSON null.
func bodyProperties(body any) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(data, &properties); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	return properties, nil
}

// writeContainers applies the configuration containers that a request
// supplies to configs, which holds the entry's stored containers, and reports
// whether the request supplied any. supplied is the request's properties, as
// bodyProperties returns them. On create a container is put in stored form.
// On PATCH it is merged into the stored container, and null removes it.
//
// A non-empty message means a container is invalid. It is the text for a 400
// response, and configs must then not be stored.
func writeContainers(configs storedconfig.Configs, supplied map[string]json.RawMessage, create bool) (bool, string, error) {
	changed := false
	for _, name := range containerNames {
		value, ok := supplied[name]
		if !ok {
			continue
		}
		changed = true
		if string(value) == "null" {
			delete(configs, name)
			continue
		}
		var err error
		if create {
			value, err = storedconfig.Normalize(name, value)
		} else {
			value, err = storedconfig.Merge(name, configs[name], value)
		}
		if err != nil {
			return false, "", err
		}
		if err := storedconfig.Check(name, value); err != nil {
			return false, err.Error(), nil
		}
		configs[name] = value
	}
	return changed, "", nil
}

// patchValue returns a specified, non-null patch value.
func patchValue[T any](patch nullable.Nullable[T]) (*T, bool) {
	value, err := patch.Get()
	if err != nil {
		return nil, false
	}
	return &value, true
}
