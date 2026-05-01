package availability

import (
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type AvailabilityCreatorImpl struct {
	illRepo                ill_db.IllRepo
	directoryLookupAdapter adapter.DirectoryLookupAdapter
}

func NewAvailabilityCreator(illRepo ill_db.IllRepo, directoryLookupAdapter adapter.DirectoryLookupAdapter) AvailabilityCreator {
	return &AvailabilityCreatorImpl{
		illRepo:                illRepo,
		directoryLookupAdapter: directoryLookupAdapter,
	}
}

func (c *AvailabilityCreatorImpl) GetAdapter(ctx common.ExtendedContext, symbol string) (AvailabilityAdapter, error) {
	if c.illRepo == nil {
		return nil, nil // No ILL repository configured, return no availability
	}
	peers, _, err := c.illRepo.GetCachedPeersBySymbols(ctx, []string{symbol}, c.directoryLookupAdapter)
	if err != nil {
		return nil, err
	}
	for _, peer := range peers {
		entry := peer.CustomData
		if entry.Z3950Config != nil {
			return NewZ3950AvailabilityAdapter(ctx, *entry.Z3950Config)
		}
	}
	return nil, nil // No availability adapter for this symbol
}
