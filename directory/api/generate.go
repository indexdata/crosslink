// Package api contains generated Directory API bindings.
//
//go:generate make -C .. generate-api
package api

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/getkin/kin-openapi/openapi3"
	_ "github.com/oapi-codegen/nullable"
	_ "github.com/oapi-codegen/runtime"
	_ "github.com/oapi-codegen/runtime/types"
)
