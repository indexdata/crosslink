package api

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

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
		require.False(t, schema.AdditionalProperties.Has != nil && *schema.AdditionalProperties.Has, name)
		require.ElementsMatch(t, []string{"type", "key", "data"}, schema.Required, name)
	}
	entryData := spec.Components.Schemas["ImportEntryData"].Value
	require.Contains(t, entryData.Required, "parent")
	require.Contains(t, entryData.Required, "lmsConfig")
	require.Contains(t, entryData.Required, "holdingsPolicy")
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
		requirePropertySchemaRef(t, contract, "ImportEntryRecord", "key", "SymbolProperties")
		requireArrayItemsSchemaRef(t, contract, "ImportEntryData", "symbols", "SymbolProperties")
		requireArrayItemsSchemaRef(t, contract, "ImportEntryData", "endpoints", "ServiceEndpointProperties")
		requireArrayItemsSchemaRef(t, contract, "ImportEntryData", "closures", "ClosureProperties")
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

func requireArrayItemsSchemaRef(t *testing.T, contract *openapi3.T, schemaName, propertyName, referencedSchemaName string) {
	t.Helper()
	items := contract.Components.Schemas[schemaName].Value.Properties[propertyName].Value.Items
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
	property := contract.Components.Schemas[schemaName].Value.Properties[propertyName]
	want := "#/components/schemas/" + referencedSchemaName
	if property.Ref == want {
		return
	}
	for _, schema := range property.Value.AllOf {
		if schema.Ref == want {
			return
		}
	}
	require.Equal(t, want, property.Ref)
}
