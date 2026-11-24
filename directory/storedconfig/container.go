package storedconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Entry API property names used as keys in Configs.
const (
	CatalogConfig   = "catalogConfig"
	DiscoveryConfig = "discoveryConfig"
	HoldingsPolicy  = "holdingsPolicy"
	IllConfig       = "illConfig"
	LmsConfig       = "lmsConfig"
)

// Normalize prepares a container for storage. It preserves a null container,
// removes null object fields at any depth, and rewrites numbers such as 10.0
// and 1e1 as 10. In illConfig it uppercases lender authorities and symbols.
//
// Fields that mean unset are omitted so they can inherit a value. These are
// empty queryConfig, metadataFormat, and metadataFormat.marc21 objects in
// catalogConfig, and empty address and fromAgency strings in lmsConfig.
func Normalize(name string, value json.RawMessage) (json.RawMessage, error) {
	decoded, err := decode(value)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	if decoded == nil {
		return json.RawMessage("null"), nil
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is not an object", name)
	}
	dropNulls(object)
	switch name {
	case CatalogConfig:
		// The resolver treats a stored key as a local value, so an empty
		// object here would block inheritance without setting anything.
		if metadata, ok := object["metadataFormat"].(map[string]any); ok {
			dropEmpty(metadata, "marc21")
		}
		dropEmpty(object, "queryConfig", "metadataFormat")
	case LmsConfig:
		// An empty address or fromAgency means unset. Stored as "", it
		// would count as a local value and hide the inherited one.
		for _, field := range []string{"address", "fromAgency"} {
			if object[field] == "" {
				delete(object, field)
			}
		}
	case IllConfig:
		lenders, _ := object["lendersOfLastResort"].([]any)
		for _, lender := range lenders {
			if symbol, ok := lender.(map[string]any); ok {
				for _, field := range []string{"authority", "symbol"} {
					if value, ok := symbol[field].(string); ok {
						symbol[field] = strings.ToUpper(value)
					}
				}
			}
		}
	}
	return json.Marshal(object)
}

// Check validates cross-field rules on a complete configuration container.
// Only catalogConfig defines such rules. Validation errors can be returned
// to the client.
func Check(name string, value json.RawMessage) error {
	decoded, err := decode(value)
	if err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	object, _ := decoded.(map[string]any)
	if name != CatalogConfig || object == nil {
		return nil
	}
	sru, hasSru := object["sru"].(map[string]any)
	zoom, hasZoom := object["zoom"].(map[string]any)
	if hasSru && hasZoom {
		return errors.New("catalogConfig cannot configure both SRU and ZOOM endpoints")
	}
	if _, ok := sru["address"]; hasSru && !ok {
		return errors.New("catalogConfig.sru.address is required when creating SRU configuration")
	}
	if _, ok := zoom["address"]; hasZoom && !ok {
		return errors.New("catalogConfig.zoom.address is required when creating ZOOM configuration")
	}
	holdings, _ := object["holdingsFormat"].(map[string]any)
	if marc, ok := holdings["marc"].(map[string]any); ok {
		rules, _ := marc["availability"].([]any)
		for _, rule := range rules {
			predicate, _ := rule.(map[string]any)
			_, hasValue := predicate["value"]
			_, hasValues := predicate["values"]
			if predicate["operator"] == "equals" && !hasValue || predicate["operator"] == "notIn" && !hasValues {
				return errors.New("catalogConfig.holdingsFormat.marc.availability requires value for equals and values for notIn")
			}
		}
	}
	if len(holdings) > 1 {
		return errors.New("catalogConfig.holdingsFormat must set at most one of marc, opac, reservoir, or marc21plus1")
	}
	return nil
}

// Merge applies an RFC 7396 merge patch and normalizes the result for storage.
// Pass nil for stored when the entry has no local container. Omitted fields
// keep their values, null deletes a field, objects merge recursively, and
// other values (including arrays) replace the stored value.
//
// catalogConfig.holdingsFormat has one extra rule because it selects a single
// parser. An empty object keeps the current selection. Naming the current
// parser updates its settings. Naming a different parser replaces it.
func Merge(name string, stored, patch json.RawMessage) (json.RawMessage, error) {
	target, err := decode(stored)
	if err != nil {
		return nil, fmt.Errorf("decode stored %s: %w", name, err)
	}
	changes, err := decode(patch)
	if err != nil {
		return nil, fmt.Errorf("decode %s patch: %w", name, err)
	}
	if name == CatalogConfig {
		target, changes = selectHoldingsParser(target, changes)
	}
	merged, err := json.Marshal(mergePatch(target, changes))
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", name, err)
	}
	return Normalize(name, merged)
}

// selectHoldingsParser adjusts the inputs in place before the recursive merge.
// It skips an empty holdingsFormat patch and removes unselected stored parsers
// when the patch names a parser.
func selectHoldingsParser(stored, patch any) (any, any) {
	changes, _ := patch.(map[string]any)
	parsers, ok := changes["holdingsFormat"].(map[string]any)
	if !ok {
		return stored, patch
	}
	if len(parsers) == 0 {
		delete(changes, "holdingsFormat")
		return stored, patch
	}
	target, _ := stored.(map[string]any)
	if current, ok := target["holdingsFormat"].(map[string]any); ok {
		for parser := range current {
			if _, named := parsers[parser]; !named {
				delete(current, parser)
			}
		}
	}
	return stored, patch
}

func mergePatch(target, patch any) any {
	changes, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	object, ok := target.(map[string]any)
	if !ok {
		object = map[string]any{}
	}
	for key, value := range changes {
		if value == nil {
			delete(object, key)
		} else {
			object[key] = mergePatch(object[key], value)
		}
	}
	return object
}

// decode treats empty input as JSON null. Decoding numbers as float64 lets
// json.Marshal write values such as 10.0 and 1e1 as 10. The schema accepts
// all three spellings, but the entry API decodes integer fields into Go
// integers, which require the integer spelling.
func decode(value json.RawMessage) (any, error) {
	if len(value) == 0 {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// dropNulls removes null members from objects at every depth.
func dropNulls(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, member := range typed {
			if member == nil {
				delete(typed, key)
			} else {
				dropNulls(member)
			}
		}
	case []any:
		for _, member := range typed {
			dropNulls(member)
		}
	}
}

// dropEmpty removes the named members of object that are empty objects.
func dropEmpty(object map[string]any, names ...string) {
	for _, name := range names {
		if member, ok := object[name].(map[string]any); ok && len(member) == 0 {
			delete(object, name)
		}
	}
}
