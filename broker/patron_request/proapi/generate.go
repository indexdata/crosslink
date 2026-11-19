// Package proapi contains generated patron-request API bindings.
//
//go:generate make -C ../.. patron_request/proapi/pr-openapi_gen.go
package proapi

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/indexdata/crosslink/iso18626"
	_ "github.com/oapi-codegen/nullable"
	_ "github.com/oapi-codegen/runtime"
	_ "github.com/oapi-codegen/runtime/types"
)
