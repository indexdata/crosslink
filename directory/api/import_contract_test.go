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
