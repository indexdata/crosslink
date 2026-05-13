package holdings

import (
	"github.com/indexdata/crosslink/broker/ill_db"
)

type AvailabilityCreator interface {
	GetAdapter(peer ill_db.Peer) (LookupAdapter, error)
}
