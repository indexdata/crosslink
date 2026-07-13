package catalog

import (
	"github.com/indexdata/crosslink/broker/ill_db"
)

type LookupAdapterCreator interface {
	GetAdapter(peer ill_db.Peer) (LookupAdapter, error)
}
