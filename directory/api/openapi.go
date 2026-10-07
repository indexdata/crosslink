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

// GetOpenapiYaml returns the complete bundled OpenAPI specification as YAML.
func (a ApiImpl) GetOpenapiYaml(_ context.Context, _ GetOpenapiYamlRequestObject) (GetOpenapiYamlResponseObject, error) {
	formats, err := bundledSpecification()
	if err != nil {
		return nil, err
	}
	return specificationResponse{contentType: "application/yaml", body: formats.yaml}, nil
}

// GetOpenapiJson returns the complete bundled OpenAPI specification as JSON.
func (a ApiImpl) GetOpenapiJson(_ context.Context, _ GetOpenapiJsonRequestObject) (GetOpenapiJsonResponseObject, error) {
	formats, err := bundledSpecification()
	if err != nil {
		return nil, err
	}
	return specificationResponse{contentType: "application/json", body: formats.json}, nil
}

type specificationResponse struct {
	contentType string
	body        []byte
}

func (response specificationResponse) VisitGetOpenapiYamlResponse(writer http.ResponseWriter) error {
	return response.write(writer)
}

func (response specificationResponse) VisitGetOpenapiJsonResponse(writer http.ResponseWriter) error {
	return response.write(writer)
}

func (response specificationResponse) write(writer http.ResponseWriter) error {
	writer.Header().Set("Content-Type", response.contentType)
	writer.WriteHeader(http.StatusOK)
	_, err := writer.Write(response.body)
	return err
}
