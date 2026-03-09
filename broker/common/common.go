package common

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/indexdata/crosslink/iso18626"
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

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldName := typ.Field(i).Name
		jsonTag, ok := typ.Field(i).Tag.Lookup("json")
		if ok {
			before, _, found := strings.Cut(jsonTag, ",")
			if found {
				fieldName = before
			} else {
				fieldName = jsonTag
			}
		}
		result[fieldName] = field.Interface()
	}

	return result, nil
}

func SamHasItems(sam iso18626.SupplyingAgencyMessage) bool {
	return strings.Contains(sam.MessageInfo.Note, MULTIPLE_ITEMS) && strings.Contains(sam.MessageInfo.Note, MULTIPLE_ITEMS_END)
}

func GetItemParams(note string) ([][]string, int, int) {
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
		result = append(result, UnpackItemsNote(f))
	}
	return result, startIdx, endIdx
}

// PackSamNote creates a note string for a SupplyingAgencyMessage containing multiple items,
// using the defined markers and escaping. Does the reverse of GetItemParams
func PackSamNote(items [][]string) string {
	var current strings.Builder
	current.WriteString(MULTIPLE_ITEMS)
	current.WriteString("\n")
	for _, item := range items {
		current.WriteString(PackItemsNote(item))
		current.WriteString("\n")
	}
	current.WriteString(MULTIPLE_ITEMS_END)
	return current.String()
}

func PackItemsNote(fields []string) string {
	escaped := make([]string, len(fields))
	for i, f := range fields {
		// Escape backslashes first, then the separator
		temp := strings.ReplaceAll(f, "\\", "\\\\")
		escaped[i] = strings.ReplaceAll(temp, "|", "\\|")
	}
	return strings.Join(escaped, "|")
}

func UnpackItemsNote(input string) []string {
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
