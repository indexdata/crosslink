package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/stretchr/testify/require"
)

// fullCatalogConfig returns a stored catalog row with every column set.
func fullCatalogConfig(t *testing.T) db.CatalogConfig {
	t.Helper()
	var original db.CatalogConfig
	v := reflect.ValueOf(&original).Elem()
	for i := 0; i < v.NumField(); i++ {
		field, name := v.Field(i), v.Type().Field(i).Name
		switch field.Interface().(type) {
		case *string:
			value := "stored-" + name
			field.Set(reflect.ValueOf(&value))
		case *bool:
			value := true
			field.Set(reflect.ValueOf(&value))
		}
	}
	original.ZoomOptions = []byte(`{"count":"10"}`)
	original.HoldingsConfig = []byte(`{"marc":{"mainField":"999"}}`)
	return original
}

// catalogColumns maps each column the upsert writes to its value in params.
func catalogColumns(params db.UpsertCatalogConfigParams) map[string]any {
	columns := map[string]any{}
	v := reflect.ValueOf(params)
	for i := 0; i < v.NumField(); i++ {
		if name := v.Type().Field(i).Name; name != "ID" && name != "Entry" {
			columns[name] = v.Field(i).Interface()
		}
	}
	return columns
}

// TestCatalogConfigPatchNullClearsSubObject checks that null on a catalog
// sub-object clears every column backing it, including the legacy holdings
// columns, and leaves all other columns as stored.
func TestCatalogConfigPatchNullClearsSubObject(t *testing.T) {
	original := fullCatalogConfig(t)
	unchanged, err := catalogConfigPatchToDBParams(uuid.New(), CatalogConfigPatch{}, original)
	require.NoError(t, err)
	stored := catalogColumns(unchanged)
	for name, value := range stored {
		require.False(t, reflect.ValueOf(value).IsNil(), "omitting every field keeps %s", name)
	}

	for _, tc := range []struct {
		field    string
		prefixes []string
	}{
		{"metadataUpdateMode", []string{"MetadataUpdateMode"}},
		{"sru", []string{"Sru"}},
		{"zoom", []string{"Zoom"}},
		{"queryConfig", []string{"Query"}},
		{"holdingsFormat", []string{"Holdings"}},
		{"metadataFormat", []string{"MetadataMarc21"}},
	} {
		t.Run(tc.field, func(t *testing.T) {
			var patch CatalogConfigPatch
			require.NoError(t, json.Unmarshal([]byte(`{"`+tc.field+`":null}`), &patch))
			params, err := catalogConfigPatchToDBParams(uuid.New(), patch, original)
			require.NoError(t, err)
			cleared := 0
			for name, value := range catalogColumns(params) {
				backing := false
				for _, prefix := range tc.prefixes {
					backing = backing || strings.HasPrefix(name, prefix)
				}
				if backing {
					cleared++
					require.True(t, reflect.ValueOf(value).IsNil(), "%s: null clears %s", tc.field, name)
				} else {
					require.Equal(t, stored[name], value, "%s: null keeps %s", tc.field, name)
				}
			}
			require.NotZero(t, cleared, "%s has backing columns", tc.field)
		})
	}
}
