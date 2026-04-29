// file: z3950.go
//go:build cgo

package availability

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
)

type Z3950AvailabilityAdapter struct {
	zurl string
}

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, symbol string) (AvailabilityAdapter, error) {
	// TODO: Z39.50 configuration
	return &Z3950AvailabilityAdapter{}, nil
}

func (a *Z3950AvailabilityAdapter) Lookup(params AvailabilityLookupParams) ([]Availability, error) {
	return nil, fmt.Errorf("not implemented")
}
