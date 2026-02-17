package oapi

import _ "embed"

// OpenAPISpecYAML contains the bundled OpenAPI specification served by the broker.
//
//go:embed open-api.yaml
var OpenAPISpecYAML []byte
