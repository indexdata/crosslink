package catalog

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type LookupAdapterCreator interface {
	GetAdapter(ctx common.ExtendedContext, peer ill_db.Peer) (LookupAdapter, error)
}
