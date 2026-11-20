package storedconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Configuration container names, as the entry API properties name them.
const (
	IllConfig      = "illConfig"
	LmsConfig      = "lmsConfig"
	CatalogConfig  = "catalogConfig"
	HoldingsPolicy = "holdingsPolicy"
)

// Normalize puts a configuration container into the form it is stored and
// read back in. It removes null-valued object members at every depth and
// writes a whole number such as 10.0 or 1e1 as 10. It also drops values that
// mean unset, which are an empty queryConfig, metadataFormat or
// metadataFormat.marc21 in catalogConfig and an empty address or fromAgency
// in lmsConfig. Lenders of last resort are put in upper case. A null
// container stays null.
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

// Check validates a stored configuration container as a whole, applying the
// rules that involve more than one field. Only catalogConfig has such rules.
// The error message is meant for the client.
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

// Merge applies a PATCH to a stored configuration container, which is nil
// when the entry stores none, and returns the merged container in stored
// form. It is an RFC 7396 merge patch, with one exception for holdingsFormat.
// An absent field is kept, null removes a field, objects merge and anything
// else replaces the stored value.
//
// The exception is that holdingsFormat holds a single parser. A patch that
// names the stored parser merges into it, a patch that names another parser
// replaces it, and an empty holdingsFormat changes nothing.
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

// selectHoldingsParser prepares a catalogConfig merge for the holdingsFormat
// rule. It changes stored and patch in place and returns them. An empty
// holdingsFormat is removed from the patch, so that it changes nothing.
// Otherwise the stored holdingsFormat loses every parser the patch does not
// name, so that the merge keeps only the named ones.
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

// decode reads JSON. Empty input decodes as null.
//
// Numbers decode as float64 and not as json.Number, so that encoding them
// again writes a whole number in integer form. The schema accepts 10.0 and
// 1e1 for an integer field, but reads decode the container into Go integers,
// which reject those forms.
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
