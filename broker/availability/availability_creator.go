package availability

import "github.com/indexdata/crosslink/broker/common"

type AvailabilityCreator interface {
	// GetAdapter returns an AvailabilityAdapter for the given symbol, or nil if no adapter is available.
	GetAdapter(ctx common.ExtendedContext, symbol string) (AvailabilityAdapter, error)
}
