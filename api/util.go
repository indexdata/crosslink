package api

import (
	"errors"
	"reflect"
	"strings"

	"github.com/oapi-codegen/nullable"
)

func maybeUpdateTxtCol(cur *string, patch nullable.Nullable[string]) *string {
	if !patch.IsSpecified() {
		return cur
	}
	if patch.IsNull() {
		return nil
	}
	patchStr := patch.MustGet()
	return &patchStr
}

func resolveCombinedSymbol(combined string) (authority, symbol string, err error) {
	colonIndex := strings.IndexByte(combined, ':')
	if colonIndex == -1 {
		return "", "", errors.New("Symbol delimeter not found")
	}
	authority = combined[:colonIndex]
	symbol = combined[colonIndex+1:]
	return
}

func derefOrDefault[T any](ptr *T, defaultValue T) T {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}

// Returns true if there is a struct in slice that has a prop with the given name
// that either is equal to value or is a point to it
// TODO: we could avoid reflection if we could somehow add a method to generated types
// fulfilling an interface like identifiedBy() and that's probably possible via custom
// templating https://github.com/oapi-codegen/oapi-codegen?tab=readme-ov-file#custom-code-generation
func elementHasProperty[T any, V comparable](s []T, propName string, value V) bool {
	if reflect.ValueOf(s).Type().Elem().Kind() != reflect.Struct {
		return false
	}

	for _, item := range s {
		field := reflect.ValueOf(item).FieldByName(propName)
		vVal := reflect.ValueOf(value)
		if field.IsValid() {
			if field.Kind() == vVal.Kind() {
				if field.Interface() == value {
					return true
				}
			} else if field.Kind() == reflect.Pointer && field.Elem().Kind() == vVal.Kind() {
				if field.Elem().Interface() == value {
					return true
				}
			}
		}
	}
	return false
}

// func NlblToPGTxt(nlbl nullable.Nullable[string]) pgtype.Text {
// 	if nlbl.IsNull() || !nlbl.IsSpecified() {
// 		return pgtype.Text{String: "", Valid: false}
// 	}
// 	return pgtype.Text{String: nlbl.MustGet(), Valid: true}
// }

// func NlblToPGUUID(nlbl nullable.Nullable[uuid.UUID]) pgtype.UUID {
// 	if nlbl.IsNull() || !nlbl.IsSpecified() {
// 		return pgtype.UUID{Bytes: [16]byte{}, Valid: false}
// 	}
// 	return pgtype.UUID{Bytes: nlbl.MustGet(), Valid: true}
// }

// func PtrToPGTxt(ptr *string) pgtype.Text {
// 	if ptr == nil {
// 		return pgtype.Text{String: "", Valid: false}
// 	}
// 	return pgtype.Text{String: *ptr, Valid: true}
// }

// func PGTxtToNlbl(pgtxt pgtype.Text) nullable.Nullable[string] {
// 	if !pgtxt.Valid {
// 		nlbl := nullable.NewNullNullable[string]()
// 		nlbl.SetUnspecified() // We don't store explicitly null strings
// 		return nlbl
// 	}
// 	return nullable.NewNullableWithValue(pgtxt.String)
// }
