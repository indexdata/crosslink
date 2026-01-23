package lms

import (
	"encoding/json"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
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

func customDataToEntry(cd map[string]any) (directory.Entry, error) {
	dataBytes, err := json.Marshal(cd)
	if err != nil {
		return directory.Entry{}, err
	}
	var entry directory.Entry
	err = json.Unmarshal(dataBytes, &entry)
	if err != nil {
		return directory.Entry{}, err
	}
	return entry, nil
}

func (l *lmsCreatorImpl) GetAdapter(ctx common.ExtendedContext, symbol string) (LmsAdapter, error) {
	peers, _, err := l.illRepo.GetCachedPeersBySymbols(ctx, []string{symbol}, l.directoryLookupAdapter)
	if err != nil {
		return nil, err
	}
	for _, peer := range peers {
		entry, err := customDataToEntry(peer.CustomData)
		if err != nil {
			return nil, err
		}
		if entry.LmsConfig != nil {
			return CreateLmsAdapterNcip(*entry.LmsConfig)
		}
	}
	return CreateLmsAdapterMockOK(), nil
}
