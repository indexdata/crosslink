package common

import (
	"fmt"
	"reflect"
	"strings"
)

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
