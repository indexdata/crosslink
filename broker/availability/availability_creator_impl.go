package availability

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type AvailabilityCreatorImpl struct {
}

func NewAvailabilityCreator() AvailabilityCreator {
	return &AvailabilityCreatorImpl{}
}

func (c *AvailabilityCreatorImpl) GetAdapter(ctx common.ExtendedContext, peer ill_db.Peer) (AvailabilityAdapter, error) {
	entry := peer.CustomData
	if entry.Z3950Config != nil {
		return NewZ3950AvailabilityAdapter(ctx, *entry.Z3950Config)
	}
	return nil, nil // No availability adapter for this symbol
}
