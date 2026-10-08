package oapi

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestUpdateSchemasRejectEmptyRequiredValues(t *testing.T) {
	spec := loadTestSpec(t)

	tests := []struct {
		name       string
		schemaName string
		value      map[string]any
	}{
		{
			name:       "template title",
			schemaName: "UpdateTemplate",
			value:      map[string]any{"title": "", "body": "Body", "contentType": "text", "labels": []any{"label"}},
		},
		{
			name:       "template body",
			schemaName: "UpdateTemplate",
			value:      map[string]any{"title": "Title", "body": "", "contentType": "text", "labels": []any{"label"}},
		},
		{
			name:       "template labels",
			schemaName: "UpdateTemplate",
			value:      map[string]any{"title": "Title", "body": "Body", "contentType": "text", "labels": []any{}},
		},
		{
			name:       "template label",
			schemaName: "UpdateTemplate",
			value:      map[string]any{"title": "Title", "body": "Body", "contentType": "text", "labels": []any{""}},
		},
		{
			name:       "batch action title",
			schemaName: "UpdateBatchAction",
			value:      map[string]any{"title": "", "schedule": "FREQ=DAILY", "batchQuery": "state==NEW"},
		},
		{
			name:       "batch action schedule",
			schemaName: "UpdateBatchAction",
			value:      map[string]any{"schedule": "", "batchQuery": "state==NEW"},
		},
		{
			name:       "batch action query",
			schemaName: "UpdateBatchAction",
			value:      map[string]any{"schedule": "FREQ=DAILY", "batchQuery": ""},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema := spec.Components.Schemas[test.schemaName]
			require.NotNil(t, schema)
			require.Error(t, schema.Value.VisitJSON(test.value))
		})
	}
}

func TestUpdateSchemasAcceptValidReplacements(t *testing.T) {
	spec := loadTestSpec(t)

	tests := []struct {
		name       string
		schemaName string
		value      map[string]any
	}{
		{
			name:       "template with optional fields omitted",
			schemaName: "UpdateTemplate",
			value:      map[string]any{"title": "Title", "body": "Body", "contentType": "text", "labels": []any{"label"}},
		},
		{
			name:       "batch action with optional fields omitted",
			schemaName: "UpdateBatchAction",
			value:      map[string]any{"schedule": "FREQ=DAILY", "batchQuery": "state==NEW"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema := spec.Components.Schemas[test.schemaName]
			require.NotNil(t, schema)
			require.NoError(t, schema.Value.VisitJSON(test.value))
		})
	}
}

func loadTestSpec(t *testing.T) *openapi3.T {
	t.Helper()
	spec, err := openapi3.NewLoader().LoadFromData(OpenAPISpecYAML)
	require.NoError(t, err)
	return spec
}
