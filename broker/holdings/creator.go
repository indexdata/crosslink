package holdings

import (
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
)

type AvailabilityCreator interface {
	GetAdapter(peer ill_db.Peer) (LookupAdapter, error)
}

type MetadataSettings struct {
	Mode   directory.MetadataUpdateMode
	Format directory.MetadataFormat
}
