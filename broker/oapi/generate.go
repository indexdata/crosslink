// Package oapi contains generated broker API bindings.
//
//go:generate make -C .. oapi/openapi_gen.go
package oapi

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/indexdata/crosslink/directory/api"
	_ "github.com/oapi-codegen/runtime"
)
