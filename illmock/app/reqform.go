package app

import (
	"fmt"
	"reflect"
	"strings"
)

// PopulateStructFromForm populates the fields of a struct based on form values
func PopulateStructFromForm(s interface{}, form map[string][]string) error {
	v := reflect.ValueOf(s).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		formTag := fieldType.Tag.Get("form")

		if formTag != "" && field.CanSet() {
			if formValues, ok := form[formTag]; ok && len(formValues) > 0 {
				field.SetString(formValues[0])
			}
		} else if field.Kind() == reflect.Struct {
			// Recursively populate nested structs
			nestedStruct := field.Addr().Interface()
			if err := PopulateStructFromForm(nestedStruct, form); err != nil {
				return err
			}
		}
	}

	return nil
}

// PopulateStructFromFormNoTags populates the fields of a struct based on form values using dot notation for nested fields
func PopulateStructFromFormNoTags(s interface{}, form map[string][]string) []error {
	v := reflect.ValueOf(s).Elem()
	var errors []error
	for key, values := range form {
		if len(values) == 0 {
			continue
		}
		errors = append(errors, setFieldValue(v, strings.Split(key, "."), values[0]))
	}
	return errors
}

// setFieldValue sets the value of a field in a struct based on a dot notation key
func setFieldValue(v reflect.Value, path []string, value string) error {
	if len(path) == 1 {
		field := v.FieldByName(path[0])
		if field.IsValid() && field.CanSet() {
			field.SetString(value)
			return nil
		} else {
			return fmt.Errorf("field %s is invalid or cannot be set", path[0])
		}
	} else if len(path) > 1 {
		v = v.FieldByName(path[0])
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		return setFieldValue(v, path[1:], value)
	} else {
		return fmt.Errorf("field path cannot be empty")
	}
}
