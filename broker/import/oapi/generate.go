// Package importoapi contains generated import API bindings.
//
//go:generate make -C ../.. import/oapi/import_openapi_gen.go
package importoapi

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/indexdata/crosslink/directory/api"
	_ "github.com/indexdata/crosslink/iso18626"
	_ "github.com/oapi-codegen/runtime"
	_ "github.com/oapi-codegen/runtime/types"
)
