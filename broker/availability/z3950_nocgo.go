// file: z3950_nocgo.go
//go:build !cgo

package availability

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
)

func cgoEnabled() bool { return false }

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config) (AvailabilityAdapter, error) {
	return nil, nil
}
