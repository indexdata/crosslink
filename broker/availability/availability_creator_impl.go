package availability

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type AvailabilityCreatorImpl struct {
	mode string
}

func NewAvailabilityCreator(mode string) AvailabilityCreator {
	return &AvailabilityCreatorImpl{
		mode: mode,
	}
}

func (c *AvailabilityCreatorImpl) GetAdapter(ctx common.ExtendedContext, peer ill_db.Peer) (AvailabilityAdapter, error) {
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
