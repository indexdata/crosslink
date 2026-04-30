// file: z3950.go
//go:build cgo

package availability

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/zoom"
)

func cgoEnabled() bool { return true }

type Z3950AvailabilityAdapter struct {
	options zoom.Options
	zurl    string
}

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config) (AvailabilityAdapter, error) {
	a := &Z3950AvailabilityAdapter{
		// default options, can be overridden by config.Options
		options: zoom.Options{
			"count":                 "10",
			"preferredRecordSyntax": "usmarc",
		},
		zurl: config.Address,
	}
	if config.Options != nil {
		for k, v := range *config.Options {
			strVal, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("invalid type for option %s: expected string", k)
			}
			a.options[k] = strVal
		}
	}
	return a, nil
}

func (a *Z3950AvailabilityAdapter) Lookup(params AvailabilityLookupParams) ([]Availability, error) {
	if a.zurl == "" {
		return nil, nil // No Z39.50 server configured for this symbol, return no availability
	}
	conn := zoom.NewConnection(a.options)
	defer conn.Close()
	err := conn.Connect(a.zurl)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Z39.50 server: %w", err)
	}
	// TODO: connect and search
	return nil, fmt.Errorf("not implemented")
}
