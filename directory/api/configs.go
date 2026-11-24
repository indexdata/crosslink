package api

import (
	"encoding/json"
	"fmt"

	"github.com/oapi-codegen/nullable"

	"github.com/indexdata/crosslink/directory/storedconfig"
)

// containerNames lists the configuration containers the entry API writes,
// in the order it processes them.
var containerNames = []string{
	storedconfig.CatalogConfig, storedconfig.DiscoveryConfig, storedconfig.HoldingsPolicy,
	storedconfig.IllConfig, storedconfig.LmsConfig,
}

// bodyProperties maps each top-level request property to its raw JSON value.
// An omitted property has no key. An explicit null has the raw value "null".
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

// writeContainers updates configs in place. Create requests normalize each
// supplied container. PATCH requests merge it with the stored value. In either
// case, an explicit null removes the container and omitted containers are kept.
//
// On success, the boolean reports whether the request included any containers.
// A non-empty message describes a validation failure for the HTTP 400 response.
// Discard partial updates on any error or validation failure.
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
