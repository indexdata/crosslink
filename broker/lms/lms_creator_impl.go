package lms

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
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
		entry := peer.CustomData
		effective, err := profiles.Resolve(entry)
		if err != nil {
			return nil, err
		}
		// Diagnostics excludes endpoints, credentials, and other sensitive settings.
		ctx.Logger().Debug("resolved host profiles", "configuration", effective.Diagnostics())
		if effective.LMS != nil && effective.LMS.Address != "" {
			return createResolvedLmsAdapterNcip(*effective.LMS)
		}
	}
	return CreateLmsAdapterMockOK(), nil
}

// CreateLmsAdapterNcip resolves LMS settings and selects the configured NCIP version.
func CreateLmsAdapterNcip(lmsConfig dirapi.LmsConfig) (LmsAdapter, error) {
	effective, err := profiles.Resolve(dirapi.Entry{LmsConfig: &lmsConfig})
	if err != nil {
		return nil, err
	}
	return createResolvedLmsAdapterNcip(*effective.LMS)
}

// createResolvedLmsAdapterNcip selects the wire version after resolving profiles.
func createResolvedLmsAdapterNcip(lmsConfig dirapi.LmsConfig) (LmsAdapter, error) {
	if lmsConfig.NcipVersion != nil {
		switch *lmsConfig.NcipVersion {
		case "1.0":
			return createResolvedLmsAdapterNcip1(lmsConfig)
		case "2.02":
		default:
			return nil, fmt.Errorf("unsupported ncipVersion %q", *lmsConfig.NcipVersion)
		}
	}
	return createResolvedLmsAdapterNcip2(lmsConfig)
}
