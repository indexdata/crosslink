package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"gopkg.in/yaml.v3"
)

type specificationFormats struct {
	json []byte
	yaml []byte
}

// Cache the bundled bytes without resolving references or changing the spec used
// by request validation. YAML nodes preserve numeric values during conversion.
var bundledSpecification = sync.OnceValues(func() (specificationFormats, error) {
	jsonData, err := GetSpecJSON()
	if err != nil {
		return specificationFormats{}, fmt.Errorf("load bundled OpenAPI specification: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(jsonData, &document); err != nil {
		return specificationFormats{}, fmt.Errorf("decode bundled OpenAPI specification: %w", err)
	}
	useBlockStyle(&document)
	yamlData, err := yaml.Marshal(&document)
	if err != nil {
		return specificationFormats{}, fmt.Errorf("encode bundled OpenAPI specification as YAML: %w", err)
	}
	return specificationFormats{json: jsonData, yaml: yamlData}, nil
})

func useBlockStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		useBlockStyle(child)
	}
}

// GetOapi returns the complete bundled OpenAPI specification in YAML or JSON.
func (a ApiImpl) GetOapi(_ context.Context, request GetOapiRequestObject) (GetOapiResponseObject, error) {
	format := Yaml
	if request.Params.Format != nil {
		format = *request.Params.Format
	}
	if format != Yaml && format != Json {
		return GetOapi400TextResponse("invalid format: expected yaml or json"), nil
	}
	formats, err := bundledSpecification()
	if err != nil {
		return nil, err
	}
	if format == Json {
		return specificationResponse{contentType: "application/json", body: formats.json}, nil
	}
	return specificationResponse{contentType: "application/yaml", body: formats.yaml}, nil
}

type specificationResponse struct {
	contentType string
	body        []byte
}

func (response specificationResponse) VisitGetOapiResponse(writer http.ResponseWriter) error {
	writer.Header().Set("Content-Type", response.contentType)
	writer.WriteHeader(http.StatusOK)
	_, err := writer.Write(response.body)
	return err
}
