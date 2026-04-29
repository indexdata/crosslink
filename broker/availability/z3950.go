// file: z3950.go
//go:build cgo

package availability

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/zoom"
)

type Z3950AvailabilityAdapter struct {
	options zoom.Options
	zurl    string
}

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, symbol string) (AvailabilityAdapter, error) {
	// TODO: Z39.50 configuration
	a := &Z3950AvailabilityAdapter{
		options: zoom.Options{
			"count":        "10",
			"recordSyntax": "USMARC",
		},
		zurl: symbol,
	}
	return a, nil
}

func (a *Z3950AvailabilityAdapter) Lookup(params AvailabilityLookupParams) ([]Availability, error) {
	conn := zoom.NewConnection(a.options)
	// TODO: connect and search
	conn.Close()
	return nil, fmt.Errorf("not implemented")
}
