// Package psoapi contains generated pull-slip API bindings.
//
//go:generate make -C ../.. pullslip/oapi/ps-openapi_gen.go
package psoapi

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/oapi-codegen/runtime"
)
