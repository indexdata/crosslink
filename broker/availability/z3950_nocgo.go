// file: z3950_nocgo.go
//go:build !cgo

package availability

import "github.com/indexdata/crosslink/broker/common"

func cgoEnabled() bool { return false }

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, symbol string) (AvailabilityAdapter, error) {
	return nil, nil
}
