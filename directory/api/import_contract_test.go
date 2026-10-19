package api

import (
	"context"
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestOpenAPI31PreservesNullableSchemas(t *testing.T) {
	for _, spec := range loadImportContracts(t) {
		require.Equal(t, "3.1.0", spec.OpenAPI)
		require.NoError(t, spec.Validate(context.Background()))

		require.NoError(t, spec.Components.Schemas["ZoomConfigPatch"].Value.Properties["options"].Value.AdditionalProperties.Schema.Value.VisitJSON(nil))
		require.NoError(t, spec.Components.Schemas["CatalogConfigPatch"].Value.Properties["profile"].Value.VisitJSON(nil))
		require.True(t, composedRequired(spec.Components.Schemas["CreateClosure"].Value, "entry"))
		require.NotContains(t, spec.Components.Schemas["AddEntry"].Value.Properties, "closures")
	}
}

func TestImportOpenAPIContract(t *testing.T) {
	spec, err := GetSpec()
	require.NoError(t, err)
	require.NoError(t, spec.Validate(context.Background()))
	sourceSpec, err := openapi3.NewLoader().LoadFromFile("../api.yaml")
	require.NoError(t, err)
	require.NoError(t, sourceSpec.Validate(context.Background()))
	operation := spec.Paths.Find("/import")
	require.NotNil(t, operation)
	require.NotNil(t, operation.Post)

	post := operation.Post
	require.Len(t, post.Parameters, 1)
	require.Equal(t, "conflictPolicy", post.Parameters[0].Value.Name)
	require.False(t, post.Parameters[0].Value.Required)
	require.Equal(t, "fail", post.Parameters[0].Value.Schema.Value.Default)
	require.NotNil(t, post.RequestBody)
	media := post.RequestBody.Value.Content.Get("application/x-ndjson")
	require.NotNil(t, media)
	require.NotNil(t, media.Schema)
	require.Contains(t, media.Extensions, "x-ndjson-item-schema")
	for _, status := range []string{"200", "400", "401", "413", "500"} {
		require.NotNil(t, post.Responses.Value(status), status)
	}

	for _, name := range []string{"ImportEntryRecord", "ImportTierRecord", "ImportNetworkRecord"} {
		schema := spec.Components.Schemas[name].Value
		require.True(t, schemaHasAdditionalPropertiesFalse(schema), name)
		require.ElementsMatch(t, []string{"type", "key", "data"}, composedRequiredNames(schema), name)
	}
	entryData := spec.Components.Schemas["ImportEntryData"].Value
	require.True(t, composedRequired(entryData, "parent"))
	require.True(t, composedRequired(entryData, "lmsConfig"))
	require.True(t, composedRequired(entryData, "holdingsPolicy"))
}

func TestImportHostSettingsMatchEntryContract(t *testing.T) {
	spec, err := GetSpec()
	require.NoError(t, err)
	source, err := openapi3.NewLoader().LoadFromFile("../api.yaml")
	require.NoError(t, err)
	for _, name := range []string{"LmsConfig", "CatalogConfig", "HoldingsParserConfig", "MarcHoldingsParserConfig", "OpacHoldingsParserConfig", "MarcAvailabilityPredicate"} {
		t.Run(name, func(t *testing.T) {
			for _, contract := range []*openapi3.T{source, spec} {
				entry := contract.Components.Schemas[name].Value
				imported := contract.Components.Schemas["Import"+name].Value
				fields := make([]string, 0, len(entry.Properties))
				for field := range entry.Properties {
					fields = append(fields, field)
					require.Contains(t, imported.Properties, field)
				}
				require.Len(t, imported.Properties, len(fields))
				require.ElementsMatch(t, fields, imported.Required)
				require.NotNil(t, imported.AdditionalProperties.Has)
				require.False(t, *imported.AdditionalProperties.Has)
			}
		})
	}
}

func TestImportContractReusesDirectoryCreationObjectsAndEnums(t *testing.T) {
	contracts := loadImportContracts(t)

	for _, contract := range contracts {
		for _, redundantAlias := range []string{"ImportEntryKey", "ImportSymbolRef", "ImportServiceEndpoint", "ImportAddressComponent", "ImportClosure"} {
			require.NotContains(t, contract.Components.Schemas, redundantAlias)
		}
		requireArrayItemsSchemaRef(t, contract, "ImportEntryData", "symbols", "ImportSymbolProperties")
		requireArrayItemsSchemaRef(t, contract, "ImportEntryData", "endpoints", "ImportServiceEndpointProperties")
		requireArrayItemsSchemaRef(t, contract, "ImportEntryData", "closures", "ImportClosureProperties")
		requireArrayItemsSchemaRef(t, contract, "ImportAddress", "addressComponents", "AddressComponent")
		requirePropertySchemaRef(t, contract, "Entry", "type", "EntryType")
		requirePropertySchemaRef(t, contract, "ImportEntryData", "type", "EntryType")
		requirePropertySchemaRef(t, contract, "Tier", "level", "TierLevel")
		requirePropertySchemaRef(t, contract, "ImportTierData", "level", "TierLevel")
		requirePropertySchemaRef(t, contract, "Tier", "type", "TierType")
		requirePropertySchemaRef(t, contract, "ImportTierData", "type", "TierType")
		requirePropertySchemaRef(t, contract, "QueryConfig", "type", "QueryConfigType")
		requirePropertySchemaRef(t, contract, "ImportQueryConfig", "type", "QueryConfigType")
		requirePropertySchemaRef(t, contract, "LmsConfig", "vendor", "HostLmsVendor")
		requirePropertySchemaRef(t, contract, "ImportLmsConfig", "vendor", "HostLmsVendor")
		requirePropertySchemaRef(t, contract, "LmsConfig", "bibIdNormalization", "BibIdNormalization")
		requirePropertySchemaRef(t, contract, "ImportLmsConfig", "bibIdNormalization", "BibIdNormalization")
		requirePropertySchemaRef(t, contract, "AddressProperties", "type", "AddressType")
		requirePropertySchemaRef(t, contract, "ImportAddress", "type", "AddressType")
		requirePropertySchemaRef(t, contract, "AddressComponent", "type", "AddressComponentType")
		requirePropertySchemaRef(t, contract, "MarcAvailabilityPredicate", "operator", "MarcAvailabilityOperator")
		requirePropertySchemaRef(t, contract, "ImportMarcAvailabilityPredicate", "operator", "MarcAvailabilityOperator")
		requirePropertySchemaRef(t, contract, "OpacHoldingsParserConfig", "availabilityRule", "OpacAvailabilityRule")
		requirePropertySchemaRef(t, contract, "ImportOpacHoldingsParserConfig", "availabilityRule", "OpacAvailabilityRule")
		requirePropertySchemaRef(t, contract, "OpacHoldingsParserConfig", "shelvingLocationSource", "ShelvingLocationSource")
		requirePropertySchemaRef(t, contract, "ImportOpacHoldingsParserConfig", "shelvingLocationSource", "ShelvingLocationSource")
	}
}

func TestCreationAndResponseSchemasShareProperties(t *testing.T) {
	for _, contract := range loadImportContracts(t) {
		tests := []struct {
			name     string
			base     string
			creation string
			response string
		}{
			{"entry", "EntryProperties", "AddEntry", "Entry"},
			{"symbol", "SymbolProperties", "AddEntrySymbol", "Symbol"},
			{"service endpoint", "ServiceEndpointProperties", "AddEntryServiceEndpoint", "ServiceEndpoint"},
			{"closure", "ClosureProperties", "CreateClosure", "Closure"},
			{"network", "NetworkProperties", "AddNetwork", "Network"},
			{"tier", "TierProperties", "AddTier", "Tier"},
			{"entry tier", "EntryTierProperties", "AddEntryTier", "EntryTier"},
			{"entry network", "EntryNetworkProperties", "AddEntryNetwork", "EntryNetwork"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				for _, schemaName := range []string{test.creation, test.response} {
					schema := contract.Components.Schemas[schemaName]
					require.NotNil(t, schema, schemaName)
					require.True(t, hasSchemaRef(schema, test.base), "%s must compose %s", schemaName, test.base)
				}
			})
		}
	}
}

func TestSharedCreationObjectsPreserveImportValidation(t *testing.T) {
	contracts := loadImportContracts(t)
	tests := []struct {
		name       string
		schemaName string
		value      any
	}{
		{
			name:       "symbol must be an object",
			schemaName: "AddEntrySymbol",
			value:      "ISIL:ABC",
		},
		{
			name:       "symbol authority must not be empty",
			schemaName: "AddEntrySymbol",
			value:      map[string]any{"authority": "", "symbol": "ABC"},
		},
		{
			name:       "symbol must not be empty",
			schemaName: "AddEntrySymbol",
			value:      map[string]any{"authority": "ISIL", "symbol": ""},
		},
		{
			name:       "symbol rejects unknown fields",
			schemaName: "AddEntrySymbol",
			value:      map[string]any{"authority": "ISIL", "symbol": "ABC", "unknown": true},
		},
		{
			name:       "import symbol rejects response id",
			schemaName: "ImportSymbolProperties",
			value:      map[string]any{"authority": "ISIL", "symbol": "ABC", "id": "40b853dd-d1de-4d30-838b-f16c938f80c0"},
		},
		{
			name:       "service endpoint rejects unknown fields",
			schemaName: "AddEntryServiceEndpoint",
			value:      map[string]any{"name": "ISO", "type": "ISO18626", "address": "https://example.test", "unknown": true},
		},
		{
			name:       "import service endpoint rejects response id",
			schemaName: "ImportServiceEndpointProperties",
			value:      map[string]any{"name": "ISO", "type": "ISO18626", "address": "https://example.test", "id": "40b853dd-d1de-4d30-838b-f16c938f80c0"},
		},
		{
			name:       "address component rejects unknown fields",
			schemaName: "AddressComponent",
			value:      map[string]any{"seq": float64(1), "type": "Locality", "value": "Copenhagen", "unknown": true},
		},
		{
			name:       "closure rejects unknown fields",
			schemaName: "CreateClosure",
			value:      map[string]any{"entry": "d6ed641d-4f2e-43f2-b78d-1e24818c884b", "startDate": "2026-01-01", "endDate": "2026-01-02", "reason": "Holiday", "unknown": true},
		},
		{
			name:       "import closure rejects response fields",
			schemaName: "ImportClosureProperties",
			value:      map[string]any{"startDate": "2026-01-01", "endDate": "2026-01-02", "reason": "Holiday", "id": "40b853dd-d1de-4d30-838b-f16c938f80c0", "entry": "d6ed641d-4f2e-43f2-b78d-1e24818c884b"},
		},
	}

	for _, contract := range contracts {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				schema := contract.Components.Schemas[test.schemaName].Value
				require.Error(t, schema.VisitJSON(test.value))
			})
		}
	}
}

func TestStrictSharedCreationObjectsDoNotRejectComposedModels(t *testing.T) {
	contracts := loadImportContracts(t)
	tests := []struct {
		name       string
		schemaName string
		value      any
	}{
		{
			name:       "symbol",
			schemaName: "Symbol",
			value:      map[string]any{"id": "40b853dd-d1de-4d30-838b-f16c938f80c0", "authority": "ISIL", "symbol": "ABC"},
		},
		{
			name:       "symbol patch",
			schemaName: "SymbolPatch",
			value:      map[string]any{"id": "40b853dd-d1de-4d30-838b-f16c938f80c0", "authority": "ISIL", "symbol": "ABC"},
		},
		{
			name:       "service endpoint",
			schemaName: "ServiceEndpoint",
			value:      map[string]any{"id": "40b853dd-d1de-4d30-838b-f16c938f80c0", "name": "ISO", "type": "ISO18626", "address": "https://example.test"},
		},
		{
			name:       "service endpoint patch",
			schemaName: "ServiceEndpointPatch",
			value:      map[string]any{"id": "40b853dd-d1de-4d30-838b-f16c938f80c0", "name": "ISO", "type": "ISO18626", "address": "https://example.test"},
		},
		{
			name:       "closure",
			schemaName: "Closure",
			value:      map[string]any{"id": "40b853dd-d1de-4d30-838b-f16c938f80c0", "entry": "d6ed641d-4f2e-43f2-b78d-1e24818c884b", "startDate": "2026-01-01", "endDate": "2026-01-02", "reason": "Holiday"},
		},
		{
			name:       "network assignment",
			schemaName: "ImportNetworkAssignment",
			value:      map[string]any{"entry": "d6ed641d-4f2e-43f2-b78d-1e24818c884b", "priority": float64(1)},
		},
	}

	for _, contract := range contracts {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				schema := contract.Components.Schemas[test.schemaName].Value
				require.NoError(t, schema.VisitJSON(test.value, openapi3.EnableFormatValidation()))
			})
		}
	}
}

func requireArrayItemsSchemaRef(t *testing.T, contract *openapi3.T, schemaName, propertyName, referencedSchemaName string) {
	t.Helper()
	property := composedProperty(contract.Components.Schemas[schemaName].Value, propertyName)
	require.NotNil(t, property, "%s.%s", schemaName, propertyName)
	items := property.Value.Items
	require.Equal(t, "#/components/schemas/"+referencedSchemaName, items.Ref)
}

func loadImportContracts(t *testing.T) []*openapi3.T {
	t.Helper()
	embedded, err := GetSpec()
	require.NoError(t, err)
	source, err := openapi3.NewLoader().LoadFromFile("../api.yaml")
	require.NoError(t, err)
	return []*openapi3.T{source, embedded}
}

func requirePropertySchemaRef(t *testing.T, contract *openapi3.T, schemaName, propertyName, referencedSchemaName string) {
	t.Helper()
	property := composedProperty(contract.Components.Schemas[schemaName].Value, propertyName)
	require.NotNil(t, property, "%s.%s", schemaName, propertyName)
	want := "#/components/schemas/" + referencedSchemaName
	if property.Ref == want {
		return
	}
	for _, schemas := range [][]*openapi3.SchemaRef{property.Value.AllOf, property.Value.AnyOf} {
		for _, schema := range schemas {
			if schema.Ref == want {
				return
			}
		}
	}
	require.Equal(t, want, property.Ref)
}

func composedProperty(schema *openapi3.Schema, propertyName string) *openapi3.SchemaRef {
	if property, ok := schema.Properties[propertyName]; ok {
		return property
	}
	for _, member := range schema.AllOf {
		if property := composedProperty(member.Value, propertyName); property != nil {
			return property
		}
	}
	return nil
}

func composedRequired(schema *openapi3.Schema, propertyName string) bool {
	if slices.Contains(schema.Required, propertyName) {
		return true
	}
	for _, member := range schema.AllOf {
		if composedRequired(member.Value, propertyName) {
			return true
		}
	}
	return false
}

func composedRequiredNames(schema *openapi3.Schema) []string {
	result := append([]string(nil), schema.Required...)
	for _, member := range schema.AllOf {
		result = append(result, composedRequiredNames(member.Value)...)
	}
	return result
}

func schemaHasAdditionalPropertiesFalse(schema *openapi3.Schema) bool {
	if schema.AdditionalProperties.Has != nil && *schema.AdditionalProperties.Has == false {
		return true
	}
	for _, member := range schema.AllOf {
		if schemaHasAdditionalPropertiesFalse(member.Value) {
			return true
		}
	}
	return false
}

func hasSchemaRef(schema *openapi3.SchemaRef, schemaName string) bool {
	if schema.Ref == "#/components/schemas/"+schemaName {
		return true
	}
	for _, member := range schema.Value.AllOf {
		if hasSchemaRef(member, schemaName) {
			return true
		}
	}
	return false
}
