package service

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
)

type LookupAdapterFactory struct {
	illRepo              ill_db.IllRepo
	dirAdapter           adapter.DirectoryLookupAdapter
	consortiumSymbol     string
	globalLookupAdapter  catalog.LookupAdapter
	lookupAdapterCreator catalog.LookupAdapterCreator
}

func NewLookupAdapterFactory(illRepo ill_db.IllRepo, dirAdapter adapter.DirectoryLookupAdapter, consortiumSymbol string, globalLookupAdapter catalog.LookupAdapter, lookupAdapterCreator catalog.LookupAdapterCreator) *LookupAdapterFactory {
	return &LookupAdapterFactory{
		illRepo:              illRepo,
		dirAdapter:           dirAdapter,
		consortiumSymbol:     consortiumSymbol,
		globalLookupAdapter:  globalLookupAdapter,
		lookupAdapterCreator: lookupAdapterCreator,
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

func (s *LookupAdapterFactory) GetAdapterRequester(ctx common.ExtendedContext, requester ill_db.Peer) (catalog.LookupAdapter, directory.Entry, error) {
	peer, err := s.resolveConfigPeer(ctx, requester)
	if err != nil {
		return nil, directory.Entry{}, err
	}
	if s.globalLookupAdapter != nil {
		return s.globalLookupAdapter, peer.CustomData, nil
	}
	if s.lookupAdapterCreator == nil {
		return nil, directory.Entry{}, fmt.Errorf("lookup adapter factory misconfigured: lookupAdapterCreator is nil")
	}
	lookupAdapter, err := s.lookupAdapterCreator.GetAdapter(peer)
	if err != nil {
		return nil, directory.Entry{}, fmt.Errorf("failed to get adapter for peer: %w", err)
	}
	return lookupAdapter, peer.CustomData, nil
}

func (s *LookupAdapterFactory) GetAdapterSupplier(ctx common.ExtendedContext, supplier ill_db.Peer) (catalog.LookupAdapter, error) {
	if s.lookupAdapterCreator == nil {
		return nil, fmt.Errorf("lookup adapter factory misconfigured: lookupAdapterCreator is nil")
	}
	return s.lookupAdapterCreator.GetAdapter(supplier)
}
