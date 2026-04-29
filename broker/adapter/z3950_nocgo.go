// file: z3950_nocgo.go
//go:build !cgo

package availability

import "github.com/indexdata/crosslink/broker/common"

type DummyAvailabilityAdapter struct {
}

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, symbol string) (AvailabilityAdapter, error) {
	return &DummyAvailabilityAdapter{}, nil
}

func (a *DummyAvailabilityAdapter) Lookup(params AvailabilityLookupParams) ([]Availability, error) {
	return nil, nil
}
