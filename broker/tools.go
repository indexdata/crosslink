//go:build tools

package tools

//build-time toolchain dependencies
import (
	_ "github.com/indexdata/xsd2goxsl"
	_ "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
)
