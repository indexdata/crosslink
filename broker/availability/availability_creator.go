package availability

import (
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type AvailabilityCreator interface {
	GetAdapter(peer ill_db.Peer) (adapter.LookupAdapter, error)
}
