package common

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

const MULTIPLE_ITEMS = "#MultipleItems#"
const MULTIPLE_ITEMS_END = "#MultipleItemsEnd#"

func StructToMap(obj any) (map[string]any, error) {
	val := reflect.ValueOf(obj)
	if !val.IsValid() {
		return nil, fmt.Errorf("input is not a struct")
	}

	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil, fmt.Errorf("input is not a struct")
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input is not a struct")
	}

	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func MapToStruct(obj map[string]any, v any) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
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

func SplitSymbol(symbol string) (string, string, error) {
	symbolParts := strings.SplitN(symbol, ":", 2)
	if len(symbolParts) != 2 {
		return "", "", fmt.Errorf("invalid symbol: %s", symbol)
	}
	return symbolParts[0], symbolParts[1], nil
}

func SplitAgencySymbol(symbol string) (string, string) {
	symbolParts := strings.SplitN(symbol, ":", 2)
	if len(symbolParts) != 2 {
		return "", symbol
	}
	return symbolParts[0], symbolParts[1]
}
