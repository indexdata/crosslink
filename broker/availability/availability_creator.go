package availability

import "github.com/indexdata/crosslink/broker/common"

type AvailabilityCreator interface {
	GetAdapter(ctx common.ExtendedContext, symbol string) (AvailabilityAdapter, error)
}
