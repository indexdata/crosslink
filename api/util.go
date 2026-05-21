//nolint:unused
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	"github.com/oapi-codegen/runtime/types"
)

func strPtr(str string) *string {
	return &str
}

func maybeUpdateCol[T any](cur *T, patch nullable.Nullable[T]) *T {
	if !patch.IsSpecified() {
		return cur
	}
	if patch.IsNull() {
		return nil
	}
	patchVal := patch.MustGet()
	return &patchVal
}

func updateIfValidDate(requestDate *types.Date, origDate pgtype.Timestamp) pgtype.Timestamp {
	if requestDate != nil {
		return DatePtrToPgTimestamp(requestDate)
	}
	return origDate
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

func derefOrDefaultPtr[T any](ptr *T, defaultValue *T) *T {
	if ptr != nil {
		return ptr
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

func unmarshalJSONArray[T any](jsonArray [][]byte) ([]T, error) {
	if jsonArray == nil {
		return nil, nil
	}
	result := make([]T, 0, len(jsonArray))
	for _, jsonBytes := range jsonArray {
		var item T
		if err := json.Unmarshal(jsonBytes, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func unmarshalJSONObject[T any](jsonBytes []byte) (*T, error) {
	if jsonBytes == nil {
		return nil, nil
	}
	var item T
	if err := json.Unmarshal(jsonBytes, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func DatePtrToPgTimestamp(d *types.Date) pgtype.Timestamp {
	if d == nil {
		return pgtype.Timestamp{Valid: false}
	}

	return pgtype.Timestamp{
		Time:  d.Time,
		Valid: true,
	}
}

func PgTimestampToDatePtr(ts pgtype.Timestamp) *types.Date {
	if !ts.Valid {
		return nil
	}

	t := ts.Time

	// Normalize to date-only (strip time component)
	date := types.Date{
		Time: time.Date(
			t.Year(),
			t.Month(),
			t.Day(),
			0, 0, 0, 0,
			t.Location(),
		),
	}

	return &date
}

func Sanitize[T any](target *T) error {
	if target == nil {
		return errors.New("target is nil")
	}

	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return errors.New("target must be a non-nil pointer to a struct")
	}

	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return errors.New("target must point to a struct")
	}

	return clearProtectedStruct(v)
}

func clearProtectedStruct(v reflect.Value) error {
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		fieldValue := v.Field(i)
		fieldType := t.Field(i)

		// Skip unexported fields.
		if fieldType.PkgPath != "" {
			continue
		}

		if fieldType.Tag.Get("protected") == "true" {
			if err := clearProtectedValue(fieldValue); err != nil {
				return fmt.Errorf("field %q: %w", fieldType.Name, err)
			}
			continue
		}

		// Only recurse into untagged nested structs/pointers to structs.
		switch fieldValue.Kind() {
		case reflect.Struct:
			if err := clearProtectedStruct(fieldValue); err != nil {
				return err
			}
		case reflect.Ptr:
			if !fieldValue.IsNil() && fieldValue.Elem().Kind() == reflect.Struct {
				if err := clearProtectedStruct(fieldValue.Elem()); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func clearProtectedValue(v reflect.Value) error {
	if !v.CanSet() {
		return errors.New("field cannot be set")
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString("")
		return nil

	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return nil

	case reflect.Array:
		// Arrays cannot have length 0 in place; zero the whole array.
		v.Set(reflect.Zero(v.Type()))
		return nil

	case reflect.Struct:
		// Protected struct => zero the whole struct, do not recurse.
		v.Set(reflect.Zero(v.Type()))
		return nil

	case reflect.Ptr:
		elemType := v.Type().Elem()

		switch elemType.Kind() {
		case reflect.String:
			ptr := reflect.New(elemType)
			ptr.Elem().SetString("")
			v.Set(ptr)
			return nil

		case reflect.Slice:
			ptr := reflect.New(elemType)
			ptr.Elem().Set(reflect.MakeSlice(elemType, 0, 0))
			v.Set(ptr)
			return nil

		case reflect.Array, reflect.Struct:
			// Protected pointer to array/struct => allocate zero value.
			ptr := reflect.New(elemType)
			ptr.Elem().Set(reflect.Zero(elemType))
			v.Set(ptr)
			return nil

		default:
			// For unsupported pointer element types, zero the pointer itself.
			v.Set(reflect.Zero(v.Type()))
			return nil
		}

	default:
		// Fallback: zero the field.
		v.Set(reflect.Zero(v.Type()))
		return nil
	}
}

func SplitQuery(queryString *string) (field *string, value *string, err error) {
	if queryString == nil {
		return nil, nil, nil
	}
	parts := strings.SplitN(*queryString, "=", 2)
	if len(parts) != 2 {
		return nil, nil, errors.New("Malformed query string")
	}
	return &parts[0], &parts[1], nil
}
