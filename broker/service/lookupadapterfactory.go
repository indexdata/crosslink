package service

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/holdings"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
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
	if s.illRepo == nil || s.dirAdapter == nil {
		return ill_db.Peer{}, fmt.Errorf("lookup adapter factory misconfigured: consortiumSymbol set but illRepo/dirAdapter is nil")
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

func (s *LookupAdapterFactory) GetLookupAdapter(ctx common.ExtendedContext, requester ill_db.Peer) (holdings.LookupAdapter, error) {
	if s.holdingsAdapter != nil {
		return s.holdingsAdapter, nil
	}
	peer, err := s.resolveConfigPeer(ctx, requester)
	if err != nil {
		return nil, err
	}
	if s.availabilityCreator == nil {
		return nil, fmt.Errorf("lookup adapter factory misconfigured: availabilityCreator is nil")
	}
	lookupAdapter, err := s.availabilityCreator.GetAdapter(peer)
	if err != nil {
		return nil, fmt.Errorf("failed to get adapter for peer: %w", err)
	}
	return lookupAdapter, nil
}

func (s *LookupAdapterFactory) GetConfigEntry(ctx common.ExtendedContext, requester ill_db.Peer) (directory.Entry, error) {
	peer, err := s.resolveConfigPeer(ctx, requester)
	if err != nil {
		return directory.Entry{}, err
	}
	return peer.CustomData, nil
}

func (s *LookupAdapterFactory) GetAdapterSupplier(ctx common.ExtendedContext, supplier ill_db.Peer) (holdings.LookupAdapter, error) {
	if s.availabilityCreator == nil {
		return nil, fmt.Errorf("lookup adapter factory misconfigured: availabilityCreator is nil")
	}
	return s.availabilityCreator.GetAdapter(supplier)
}
