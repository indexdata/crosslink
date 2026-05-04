package availability

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type AvailabilityCreator interface {
	GetAdapter(ctx common.ExtendedContext, peer ill_db.Peer) (AvailabilityAdapter, error)
}
