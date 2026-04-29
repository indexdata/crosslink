package availability

import "github.com/indexdata/crosslink/broker/common"

func CreateAvailabilityAdapter(ctx common.ExtendedContext, symbol string) (AvailabilityAdapter, error) {
	adapter, err := NewZ3950AvailabilityAdapter(ctx, symbol)
	if err != nil {
		return nil, err
	}
	return adapter, nil
}
