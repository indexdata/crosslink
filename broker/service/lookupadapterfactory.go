package service

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	dirapi "github.com/indexdata/crosslink/directory/api"
)

type LookupAdapterFactory struct {
	illRepo              ill_db.IllRepo
	dirAdapter           adapter.DirectoryLookupAdapter
	consortiumSymbol     string
	sources              []catalog.HoldingsSource
	lookupAdapterCreator catalog.LookupAdapterCreator
}

// NewLookupAdapterFactory creates a factory whose requester lookups try sources in order.
// A source without an adapter is resolved per peer through lookupAdapterCreator.
func NewLookupAdapterFactory(illRepo ill_db.IllRepo, dirAdapter adapter.DirectoryLookupAdapter, consortiumSymbol string, sources []catalog.HoldingsSource, lookupAdapterCreator catalog.LookupAdapterCreator) *LookupAdapterFactory {
	return &LookupAdapterFactory{
		illRepo:              illRepo,
		dirAdapter:           dirAdapter,
		consortiumSymbol:     consortiumSymbol,
		sources:              sources,
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

// GetAdapterRequester returns the lookup adapter for requester-side lookups and the
// configuration peer's Directory entry. With several resolved sources, the adapter
// tries them in order (see catalog.ChainLookupAdapter). Results report their
// configuration source (catalog.SourcedLookupResult). A nil adapter means no
// source is available for the requester.
func (s *LookupAdapterFactory) GetAdapterRequester(ctx common.ExtendedContext, requester ill_db.Peer) (catalog.LookupAdapter, dirapi.Entry, error) {
	lookupAdapter, source, entry, err := s.getAdapterRequester(ctx, requester)
	if lookupAdapter != nil && source != "" {
		// a single adapter: chain it so its results also report their source
		lookupAdapter = catalog.NewChainLookupAdapter([]catalog.ChainEntry{{Source: source, Adapter: lookupAdapter}})
	}
	return lookupAdapter, entry, err
}

// getAdapterRequester also returns the configuration source of a single resolved
// adapter; it is empty for a chain, whose results report their own source.
func (s *LookupAdapterFactory) getAdapterRequester(ctx common.ExtendedContext, requester ill_db.Peer) (catalog.LookupAdapter, string, dirapi.Entry, error) {
	peer, err := s.resolveConfigPeer(ctx, requester)
	if err != nil {
		return nil, "", dirapi.Entry{}, err
	}
	var entries []catalog.ChainEntry
	for _, source := range s.sources {
		if source.Adapter != nil {
			entries = append(entries, catalog.ChainEntry{Source: catalog.ConfigurationSourceGlobal, Adapter: source.Adapter})
			continue
		}
		if s.lookupAdapterCreator == nil {
			return nil, "", dirapi.Entry{}, fmt.Errorf("lookup adapter factory misconfigured: lookupAdapterCreator is nil")
		}
		lookupAdapter, err := s.lookupAdapterCreator.GetAdapter(ctx, peer)
		if err != nil {
			err = fmt.Errorf("failed to get adapter for peer: %w: %w", catalog.ErrLookupAdapterConfig, err)
			if len(entries) == 0 {
				return nil, "", dirapi.Entry{}, err
			}
			// An earlier source may still return holdings, so report this
			// configuration error only if the chain reaches it.
			lookupAdapter = failedLookupAdapter{err: err}
		}
		if lookupAdapter != nil {
			entries = append(entries, catalog.ChainEntry{Source: catalog.ConfigurationSourceDirectory, Adapter: lookupAdapter})
		}
	}
	switch len(entries) {
	case 0:
		return nil, "", peer.CustomData, nil
	case 1:
		return entries[0].Adapter, entries[0].Source, peer.CustomData, nil
	default:
		return catalog.NewChainLookupAdapter(entries), "", peer.CustomData, nil
	}
}

// hasDirectorySource reports whether any source is resolved from Directory configuration.
func (s *LookupAdapterFactory) hasDirectorySource() bool {
	for _, source := range s.sources {
		if source.Adapter == nil {
			return true
		}
	}
	return false
}

func (s *LookupAdapterFactory) GetAdapterSupplier(ctx common.ExtendedContext, supplier ill_db.Peer) (catalog.LookupAdapter, error) {
	if s.lookupAdapterCreator == nil {
		return nil, fmt.Errorf("lookup adapter factory misconfigured: lookupAdapterCreator is nil")
	}
	return s.lookupAdapterCreator.GetAdapter(ctx, supplier)
}

// loadBalancingPolicy uses only consortium configuration, never requester settings.
func (f *LookupAdapterFactory) loadBalancingPolicy(configPeer dirapi.Entry) (dirapi.LoadBalancingPolicy, error) {
	if f.consortiumSymbol == "" || configPeer.IllConfig == nil {
		return dirapi.LoadBalancingPolicyProportional, nil
	}
	policy, err := configPeer.IllConfig.LoadBalancingPolicy.Get()
	if err != nil {
		return dirapi.LoadBalancingPolicyProportional, nil
	}
	if !policy.Valid() {
		return "", fmt.Errorf("consortium %s has invalid loadBalancingPolicy %q", f.consortiumSymbol, policy)
	}
	return policy, nil
}

// failedLookupAdapter defers an adapter construction error until a lookup reaches it.
type failedLookupAdapter struct {
	err error
}

func (a failedLookupAdapter) Lookup(catalog.LookupParams) (catalog.LookupResult, error) {
	return nil, a.err
}
