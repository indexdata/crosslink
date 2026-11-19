// Package schedoapi contains generated scheduler API bindings.
//
//go:generate make -C ../.. scheduler/oapi/sched_openapi_gen.go
package schedoapi

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/oapi-codegen/runtime"
)
