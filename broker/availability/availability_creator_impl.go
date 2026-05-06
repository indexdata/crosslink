package availability

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

const (
	AvailabilityAdapterZoom string = "zoom" // yaz zoom adapter
	AvailabilityAdapterMock string = "mock" // mock adapter for testing
)

type AvailabilityCreatorImpl struct {
	mode string
}

func NewAvailabilityCreator(mode string) AvailabilityCreator {
	return &AvailabilityCreatorImpl{
		mode: mode,
	}
}

func (c *AvailabilityCreatorImpl) GetAdapter(ctx common.ExtendedContext, peer ill_db.Peer) (adapter.HoldingsLookupAdapter, error) {
	entry := peer.CustomData
	switch c.mode {
	case AvailabilityAdapterMock:
		if entry.Z3950Config != nil {
			return NewMockAvailabilityAdapter(*entry.Z3950Config)
		}
	case AvailabilityAdapterZoom:
		if entry.Z3950Config != nil {
			return NewZ3950AvailabilityAdapter(ctx, *entry.Z3950Config)
		}
	default:
		return nil, fmt.Errorf("bad value for %s", c.mode)
	}
	return nil, nil // No availability adapter for this peer
}
