package common

import (
	"fmt"
	"reflect"
	"strings"
)

const MULTIPLE_ITEMS = "#MultipleItems#"
const MULTIPLE_ITEMS_END = "#MultipleItemsEnd#"

func StructToMap(obj interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	val := reflect.ValueOf(obj)
	typ := reflect.TypeOf(obj)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
		typ = typ.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input is not a struct")
	}

	if err := structToMap(result, val, typ); err != nil {
		return nil, err
	}
	return result, nil
}

func structToMap(result map[string]interface{}, val reflect.Value, typ reflect.Type) error {
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		structField := typ.Field(i)
		fieldName := structField.Name
		jsonTag, ok := structField.Tag.Lookup("json")
		tagName := ""
		tagOpts := ""
		if ok {
			before, after, found := strings.Cut(jsonTag, ",")
			if before == "-" {
				continue
			}
			tagName = before
			if found {
				tagOpts = after
			}
			if found {
				fieldName = before
			} else {
				fieldName = jsonTag
			}
		}

		if structField.Anonymous && (tagName == "") {
			fieldVal := field
			fieldTyp := field.Type()
			if fieldVal.Kind() == reflect.Ptr {
				if fieldVal.IsNil() {
					continue
				}
				fieldVal = fieldVal.Elem()
				fieldTyp = fieldTyp.Elem()
			}
			if fieldVal.Kind() == reflect.Struct {
				if err := structToMap(result, fieldVal, fieldTyp); err != nil {
					return err
				}
				continue
			}
		}

		if hasTagOption(tagOpts, "omitempty") && field.IsZero() {
			continue
		}

		result[fieldName] = field.Interface()
	}
	return nil
}

func hasTagOption(options string, opt string) bool {
	if options == "" {
		return false
	}
	for _, item := range strings.Split(options, ",") {
		if item == opt {
			return true
		}
	}
	return false
}

func UnpackItemsNote(note string) ([][]string, int, int) {
	startIdx := strings.Index(note, MULTIPLE_ITEMS)
	endIdx := strings.Index(note, MULTIPLE_ITEMS_END)

	// Validate indices to avoid panics if markers are missing or misordered.
	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		return nil, startIdx, endIdx
	}

	content := note[startIdx+len(MULTIPLE_ITEMS) : endIdx]
	content = strings.TrimSpace(content)
	var result [][]string
	for _, f := range strings.Split(content, "\n") {
		result = append(result, UnpackItemNote(f))
	}
	return result, startIdx, endIdx
}

// PackItemsNote creates a note string for a SupplyingAgencyMessage containing multiple items,
// using the defined markers and escaping. Does the reverse of UnpackItemsNote.
func PackItemsNote(items [][]string) string {
	var current strings.Builder
	current.WriteString(MULTIPLE_ITEMS)
	current.WriteString("\n")
	for _, item := range items {
		current.WriteString(PackItemNote(item))
		current.WriteString("\n")
	}
	current.WriteString(MULTIPLE_ITEMS_END)
	return current.String()
}

func PackItemNote(fields []string) string {
	escaped := make([]string, len(fields))
	for i, f := range fields {
		// Escape backslashes first, then the separator
		temp := strings.ReplaceAll(f, "\\", "\\\\")
		escaped[i] = strings.ReplaceAll(temp, "|", "\\|")
	}
	return strings.Join(escaped, "|")
}

func UnpackItemNote(input string) []string {
	var result []string
	var current strings.Builder
	escaped := false

	for i := 0; i < len(input); i++ {
		char := input[i]

		if escaped {
			current.WriteByte(char)
			escaped = false
			continue
		}

		switch char {
		case '\\':
			escaped = true
		case '|':
			result = append(result, current.String())
			current.Reset()
		default:
			current.WriteByte(char)
		}
	}
	result = append(result, current.String())
	return result
}
