package service

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/holdings"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
)

type HoldingsAdapterFactory struct {
	illRepo             ill_db.IllRepo
	dirAdapter          adapter.DirectoryLookupAdapter
	consortiumSymbol    string
	holdingsAdapter     holdings.LookupAdapter
	availabilityCreator holdings.AvailabilityCreator
}

func NewHoldingsAdapterFactory(illRepo ill_db.IllRepo, dirAdapter adapter.DirectoryLookupAdapter, consortiumSymbol string, holdingsAdapter holdings.LookupAdapter, availabilityCreator holdings.AvailabilityCreator) *HoldingsAdapterFactory {
	return &HoldingsAdapterFactory{
		illRepo:             illRepo,
		dirAdapter:          dirAdapter,
		consortiumSymbol:    consortiumSymbol,
		holdingsAdapter:     holdingsAdapter,
		availabilityCreator: availabilityCreator,
	}
}

// resolveConfigPeer determines which peer provides configuration for the requester.
// If a consortiumSymbol is configured, it looks up that peer; otherwise it returns the requester.
func (s *HoldingsAdapterFactory) resolveConfigPeer(ctx common.ExtendedContext, requester ill_db.Peer) (ill_db.Peer, error) {
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
func (s *HoldingsAdapterFactory) GetLookupAdapter(ctx common.ExtendedContext, requester ill_db.Peer) (holdings.LookupAdapter, error) {
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
func (s *HoldingsAdapterFactory) GetConfigEntry(ctx common.ExtendedContext, requester ill_db.Peer) (directory.Entry, error) {
	peer, err := s.resolveConfigPeer(ctx, requester)
	if err != nil {
		return directory.Entry{}, err
	}
	return peer.CustomData, nil
}

func (s *HoldingsAdapterFactory) GetAdapterSupplier(ctx common.ExtendedContext, supplier ill_db.Peer) (holdings.LookupAdapter, error) {
	return s.availabilityCreator.GetAdapter(supplier)
}
