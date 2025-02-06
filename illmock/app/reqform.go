package app

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// BindFormWithTags populates the tagged fields of a struct based on form values
func BindFormWithTags(s interface{}, form map[string][]string) error {
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
			if err := BindFormWithTags(nestedStruct, form); err != nil {
				return err
			}
		}
	}

	return nil
}

// BindForm populates the fields of a struct based on form values using dot notation for nested fields
func BindForm(s interface{}, form map[string][]string) []error {
	v := reflect.ValueOf(s).Elem()
	var errors []error
	for key, values := range form {
		if len(values) == 0 {
			continue
		}
		err := setFieldValue(v, strings.Split(key, "."), values[0])
		if err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

// setFieldValue sets the value of a field in a struct based on a dot notation key
func setFieldValue(v reflect.Value, path []string, value string) error {
	if len(path) == 0 {
		return fmt.Errorf("field path cannot be empty")
	}

	// Ensure v is a struct or a pointer to a struct
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("value is not a struct")
	}

	field := v.FieldByName(path[0])
	if !field.IsValid() {
		return fmt.Errorf("field %s is not valid", path[0])
	}

	if len(path) == 1 {
		if field.CanSet() {
			switch kind := field.Kind(); kind {
			case reflect.String:
				field.SetString(value)
			case reflect.Int:
				iv, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return err
				}
				field.SetInt(iv)
			default:
				return fmt.Errorf("Cannot set value of field type %s at %s", kind, path[0])
			}
			return nil
		} else {
			return fmt.Errorf("field %s cannot be set", path[0])
		}
	}

	// Handle nested structs
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	return setFieldValue(field, path[1:], value)
}
