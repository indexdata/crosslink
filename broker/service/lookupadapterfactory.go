package service

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/holdings"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
)

type LookupAdapterFactory struct {
	illRepo             ill_db.IllRepo
	dirAdapter          adapter.DirectoryLookupAdapter
	consortiumSymbol    string
	holdingsAdapter     holdings.LookupAdapter
	availabilityCreator holdings.AvailabilityCreator
}

func NewLookupAdapterFactory(illRepo ill_db.IllRepo, dirAdapter adapter.DirectoryLookupAdapter, consortiumSymbol string, holdingsAdapter holdings.LookupAdapter, availabilityCreator holdings.AvailabilityCreator) *LookupAdapterFactory {
	return &LookupAdapterFactory{
		illRepo:             illRepo,
		dirAdapter:          dirAdapter,
		consortiumSymbol:    consortiumSymbol,
		holdingsAdapter:     holdingsAdapter,
		availabilityCreator: availabilityCreator,
	}
}

// resolveConfigPeer determines which peer provides configuration for the requester.
// If a consortiumSymbol is configured, it looks up that peer; otherwise it returns the requester.
func (s *LookupAdapterFactory) resolveConfigPeer(ctx common.ExtendedContext, requester ill_db.Peer) (ill_db.Peer, error) {
	if s.consortiumSymbol == "" {
		return requester, nil
	}
	consortiumPeers, _, err := s.illRepo.GetCachedPeersBySymbols(ctx, []string{s.consortiumSymbol}, s.dirAdapter) // trigger caching of consortium peer
	if err != nil {
		return ill_db.Peer{}, fmt.Errorf("failed to lookup consortium peer: %w", err)
	}
	if len(consortiumPeers) == 0 {
		return ill_db.Peer{}, fmt.Errorf("no peer found for consortium symbol '%s'", s.consortiumSymbol)
	}
	if len(consortiumPeers) > 1 {
		ctx.Logger().Warn("multiple peers found for consortium symbol, using first peer", "consortiumSymbol", s.consortiumSymbol, "peerCount", len(consortiumPeers))
	}
	return consortiumPeers[0], nil
}

// GetLookupAdapter returns the holdings lookup adapter for the requester.
// If a holdingsAdapter was directly configured it is returned as-is; otherwise the adapter
// is derived from the resolved config peer (consortium peer or requester).
// Returns nil without error when the resolved peer has no holdings adapter configured.
func (s *LookupAdapterFactory) GetLookupAdapter(ctx common.ExtendedContext, requester ill_db.Peer) (holdings.LookupAdapter, error) {
	if s.holdingsAdapter != nil {
		return s.holdingsAdapter, nil
	}
	peer, err := s.resolveConfigPeer(ctx, requester)
	if err != nil {
		return nil, err
	}
	adapter, err := s.availabilityCreator.GetAdapter(peer)
	if err != nil {
		return nil, fmt.Errorf("failed to get adapter for peer: %w", err)
	}
	return adapter, nil
}

// GetConfigEntry returns the directory.Entry that provides consortium-level configuration
// (e.g. LenderOfLastResort, HoldingsConfig) for the requester.
func (s *LookupAdapterFactory) GetConfigEntry(ctx common.ExtendedContext, requester ill_db.Peer) (directory.Entry, error) {
	peer, err := s.resolveConfigPeer(ctx, requester)
	if err != nil {
		return directory.Entry{}, err
	}
	return peer.CustomData, nil
}

func (s *LookupAdapterFactory) GetAdapterSupplier(ctx common.ExtendedContext, supplier ill_db.Peer) (holdings.LookupAdapter, error) {
	return s.availabilityCreator.GetAdapter(supplier)
}

func fixupBibliograhpicItem(info *[]iso18626.BibliographicItemId, code string, value string, replace bool) {
	for i, id := range *info {
		if id.BibliographicItemIdentifierCode.Text == code {
			if replace {
				(*info)[i].BibliographicItemIdentifier = value
			}
			return
		}
	}
	*info = append(*info, iso18626.BibliographicItemId{
		BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: code},
		BibliographicItemIdentifier:     value,
	})
}

func (a *LookupAdapterFactory) MetadataUpdate(ctx common.ExtendedContext, illRequest *iso18626.Request, requesterPeer ill_db.Peer) error {
	lookupAdapter, err := a.GetLookupAdapter(ctx, requesterPeer)
	if err != nil {
		return fmt.Errorf("failed to get lookup adapter: %w", err)
	}
	if lookupAdapter == nil {
		return nil
	}
	configPeer, err := a.GetConfigEntry(ctx, requesterPeer)
	if err != nil {
		return fmt.Errorf("failed to get config entry: %w", err)
	}
	mode := directory.None
	if configPeer.HoldingsConfig != nil && configPeer.HoldingsConfig.MetadataUpdateMode != nil {
		mode = *configPeer.HoldingsConfig.MetadataUpdateMode
	}
	if mode == directory.None {
		return nil
	}
	lookupParams := holdings.LookupParamsFromBibliographicInfo(illRequest.BibliographicInfo, illRequest.ServiceInfo)
	metadata, err := lookupAdapter.MetadataLookup(lookupParams)
	if err != nil {
		return fmt.Errorf("failed to lookup metadata: %w", err)
	}
	if mode == directory.Auto && lookupParams.Identifier != "" {
		mode = directory.Replace
	} else {
		mode = directory.Merge
	}
	switch mode {
	case directory.Replace:
		illRequest.BibliographicInfo.Title = metadata.Title
		illRequest.BibliographicInfo.Subtitle = metadata.Subtitle
		illRequest.BibliographicInfo.Author = metadata.Author
		illRequest.BibliographicInfo.SupplierUniqueRecordId = metadata.Identifier
		fixupBibliograhpicItem(&illRequest.BibliographicInfo.BibliographicItemId, "ISBN", metadata.Isbn, true)
		fixupBibliograhpicItem(&illRequest.BibliographicInfo.BibliographicItemId, "ISSN", metadata.Issn, true)
	case directory.Merge:
		if illRequest.BibliographicInfo.Title == "" {
			illRequest.BibliographicInfo.Title = metadata.Title
		}
		if illRequest.BibliographicInfo.Subtitle == "" {
			illRequest.BibliographicInfo.Subtitle = metadata.Subtitle
		}
		if illRequest.BibliographicInfo.Author == "" {
			illRequest.BibliographicInfo.Author = metadata.Author
		}
		if illRequest.BibliographicInfo.SupplierUniqueRecordId == "" {
			illRequest.BibliographicInfo.SupplierUniqueRecordId = metadata.Identifier
		}
		fixupBibliograhpicItem(&illRequest.BibliographicInfo.BibliographicItemId, "ISBN", metadata.Isbn, false)
		fixupBibliograhpicItem(&illRequest.BibliographicInfo.BibliographicItemId, "ISSN", metadata.Issn, false)
	}
	return nil
}
