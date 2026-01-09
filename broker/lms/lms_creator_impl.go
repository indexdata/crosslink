package lms

import (
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type lmsCreatorImpl struct {
	illRepo                ill_db.IllRepo
	directoryLookupAdapter adapter.DirectoryLookupAdapter
}

func NewLmsCreator(illRepo ill_db.IllRepo, directoryLookupAdapter adapter.DirectoryLookupAdapter) LmsCreator {
	return &lmsCreatorImpl{
		illRepo:                illRepo,
		directoryLookupAdapter: directoryLookupAdapter,
	}
}

func (l *lmsCreatorImpl) GetAdapter(ctx common.ExtendedContext, symbol string) (LmsAdapter, error) {
	peers, _, err := l.illRepo.GetCachedPeersBySymbols(ctx, []string{symbol}, l.directoryLookupAdapter)
	if err != nil {
		return nil, err
	}
	for _, peer := range peers {
		ncipInfo, ok := peer.CustomData["ncip"].(map[string]any)
		if ok {
			return CreateLmsAdapterNcip(ncipInfo)
		}
	}
	return CreateLmsAdapterMockOK(), nil
}
