//go:build tools

package tools

//build-time toolchain dependencies
import (
	_ "github.com/indexdata/xsd2goxsl"
)
