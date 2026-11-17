package storedconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Section names, as the entry API properties name them.
const (
	IllConfig      = "illConfig"
	LmsConfig      = "lmsConfig"
	CatalogConfig  = "catalogConfig"
	HoldingsPolicy = "holdingsPolicy"
)

// Normalize puts a configuration section into the form it is stored and read
// back in. Nulls are dropped at every depth, whole numbers are written as
// integers, fields that a stored value cannot hold empty are dropped when they
// are empty, and lenders of last resort are upper case. A null section stays
// null.
func Normalize(section string, value json.RawMessage) (json.RawMessage, error) {
	decoded, err := decode(value)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", section, err)
	}
	if decoded == nil {
		return json.RawMessage("null"), nil
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is not an object", section)
	}
	dropNulls(object)
	writeIntegers(object)
	switch section {
	case CatalogConfig:
		// The resolver treats a stored key as a local value, so an empty
		// object here would block inheritance without setting anything.
		if metadata, ok := object["metadataFormat"].(map[string]any); ok {
			dropEmpty(metadata, "marc21")
		}
		dropEmpty(object, "queryConfig", "metadataFormat")
	case LmsConfig:
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

// Check runs the checks on a whole stored section that span more than one
// field. The error message is meant for the client.
func Check(section string, value json.RawMessage) error {
	decoded, err := decode(value)
	if err != nil {
		return fmt.Errorf("decode %s: %w", section, err)
	}
	object, _ := decoded.(map[string]any)
	if section != CatalogConfig || object == nil {
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

// Merge applies a PATCH to a stored section, which is nil when the entry
// stores none, and returns the merged section in stored form. It is an RFC
// 7396 merge patch: an absent field is kept, null removes a field, objects
// merge and anything else replaces the stored value.
//
// holdingsFormat holds a single parser. A patch that names the stored parser
// merges into it, a patch that names another parser replaces it, and an empty
// holdingsFormat changes nothing.
func Merge(section string, stored, patch json.RawMessage) (json.RawMessage, error) {
	target, err := decode(stored)
	if err != nil {
		return nil, fmt.Errorf("decode stored %s: %w", section, err)
	}
	changes, err := decode(patch)
	if err != nil {
		return nil, fmt.Errorf("decode %s patch: %w", section, err)
	}
	if section == CatalogConfig {
		target, changes = selectHoldingsParser(target, changes)
	}
	merged, err := json.Marshal(mergePatch(target, changes))
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", section, err)
	}
	return Normalize(section, merged)
}

// selectHoldingsParser narrows the stored holdingsFormat to the parsers the
// patch names, so that the merge keeps only those.
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

// decode reads JSON keeping numbers as written. Empty input decodes as null.
func decode(value json.RawMessage) (any, error) {
	if len(value) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
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

// writeIntegers rewrites whole numbers written with a fraction or exponent,
// such as 10.0 or 1e1, in integer form at every depth. The schema accepts
// those forms for integer fields, but reads decode the section into Go
// integers, which reject them.
func writeIntegers(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, member := range typed {
			if number, ok := member.(json.Number); ok {
				typed[key] = integerForm(number)
			} else {
				writeIntegers(member)
			}
		}
	case []any:
		for i, member := range typed {
			if number, ok := member.(json.Number); ok {
				typed[i] = integerForm(number)
			} else {
				writeIntegers(member)
			}
		}
	}
}

// integerForm returns number in integer form when it is a whole number that
// float64 holds exactly, and otherwise as written.
func integerForm(number json.Number) json.Number {
	if !strings.ContainsAny(string(number), ".eE") {
		return number
	}
	value, err := strconv.ParseFloat(string(number), 64)
	if err != nil || value != math.Trunc(value) || math.Abs(value) > 1<<53 {
		return number
	}
	return json.Number(strconv.FormatInt(int64(value), 10))
}

// dropEmpty removes the named members of object that are empty objects.
func dropEmpty(object map[string]any, names ...string) {
	for _, name := range names {
		if member, ok := object[name].(map[string]any); ok && len(member) == 0 {
			delete(object, name)
		}
	}
}
