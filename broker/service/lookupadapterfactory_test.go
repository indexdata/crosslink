package service

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/require"
)

// globalSources configures adapter as the only, shared holdings source.
func globalSources(adapter catalog.LookupAdapter) []catalog.HoldingsSource {
	return []catalog.HoldingsSource{{Name: catalog.HoldingsAdapterMock, Adapter: adapter}}
}

// directorySources configures Directory catalog configuration as the only holdings source.
func directorySources() []catalog.HoldingsSource {
	return []catalog.HoldingsSource{{Name: catalog.HoldingsAdapterConsortium}}
}

type peerLookupCreator struct {
	adapters map[string]catalog.LookupAdapter
	errs     map[string]error
}

func (c peerLookupCreator) GetAdapter(_ common.ExtendedContext, peer ill_db.Peer) (catalog.LookupAdapter, error) {
	return c.adapters[peer.ID], c.errs[peer.ID]
}

func TestLookupAdapterFactoryOrderedSources(t *testing.T) {
	global := &catalog.MockLookupAdapter{Holdings: []catalog.Holding{{Symbol: "ISIL:GLOBAL"}}}
	directory := &catalog.MockLookupAdapter{Holdings: []catalog.Holding{{Symbol: "ISIL:DIR"}}}
	creator := peerLookupCreator{adapters: map[string]catalog.LookupAdapter{"configured": directory}}
	sources := []catalog.HoldingsSource{{Name: catalog.HoldingsAdapterConsortium}, {Name: catalog.HoldingsAdapterMock, Adapter: global}}
	factory := NewLookupAdapterFactory(nil, nil, "", sources, creator)

	t.Run("peer without directory adapter uses next source directly", func(t *testing.T) {
		lookupAdapter, source, entry, err := factory.getAdapterRequester(appCtx, ill_db.Peer{ID: "unconfigured", CustomData: dirapi.Entry{Name: "peer"}})
		require.NoError(t, err)
		require.Same(t, global, lookupAdapter)
		require.Equal(t, catalog.ConfigurationSourceGlobal, source)
		require.Equal(t, "peer", entry.Name)
	})

	t.Run("peer with directory adapter chains in configured order", func(t *testing.T) {
		lookupAdapter, source, _, err := factory.getAdapterRequester(appCtx, ill_db.Peer{ID: "configured"})
		require.NoError(t, err)
		require.Empty(t, source)
		result, err := lookupAdapter.Lookup(catalog.LookupParams{})
		require.NoError(t, err)
		holdings, err := result.GetHoldings()
		require.NoError(t, err)
		require.Equal(t, "ISIL:DIR", holdings[0].Symbol)
		require.Equal(t, catalog.ConfigurationSourceDirectory, result.(catalog.SourcedLookupResult).ConfigurationSource())
	})

	t.Run("no resolved source", func(t *testing.T) {
		factory := NewLookupAdapterFactory(nil, nil, "", directorySources(), creator)
		requesterAdapter, _, err := factory.GetAdapterRequester(appCtx, ill_db.Peer{ID: "unconfigured"})
		require.NoError(t, err)
		require.Nil(t, requesterAdapter)
	})

	t.Run("global sources need no creator", func(t *testing.T) {
		factory := NewLookupAdapterFactory(nil, nil, "", globalSources(global), nil)
		lookupAdapter, _, err := factory.GetAdapterRequester(appCtx, ill_db.Peer{})
		require.NoError(t, err)
		result, err := lookupAdapter.Lookup(catalog.LookupParams{})
		require.NoError(t, err)
		require.Equal(t, catalog.ConfigurationSourceGlobal, result.(catalog.SourcedLookupResult).ConfigurationSource())
	})

	t.Run("directory source needs creator", func(t *testing.T) {
		factory := NewLookupAdapterFactory(nil, nil, "", sources, nil)
		_, _, err := factory.GetAdapterRequester(appCtx, ill_db.Peer{})
		require.ErrorContains(t, err, "lookupAdapterCreator is nil")
	})
}

func TestLookupAdapterFactoryDefersFallbackConfigError(t *testing.T) {
	configErr := errors.New("METAPROXY_URL must be set")
	creator := peerLookupCreator{errs: map[string]error{"broken": configErr}}
	broken := ill_db.Peer{ID: "broken"}

	t.Run("earlier source with holdings succeeds", func(t *testing.T) {
		global := &catalog.MockLookupAdapter{Holdings: []catalog.Holding{{Symbol: "ISIL:GLOBAL"}}}
		sources := []catalog.HoldingsSource{{Name: catalog.HoldingsAdapterSru, Adapter: global}, {Name: catalog.HoldingsAdapterConsortium}}
		lookupAdapter, _, err := NewLookupAdapterFactory(nil, nil, "", sources, creator).GetAdapterRequester(appCtx, broken)
		require.NoError(t, err)
		result, err := lookupAdapter.Lookup(catalog.LookupParams{})
		require.NoError(t, err)
		holdings, err := result.GetHoldings()
		require.NoError(t, err)
		require.Equal(t, "ISIL:GLOBAL", holdings[0].Symbol)
	})

	t.Run("reached fallback reports config error", func(t *testing.T) {
		sources := []catalog.HoldingsSource{{Name: catalog.HoldingsAdapterSru, Adapter: &catalog.MockLookupAdapter{}}, {Name: catalog.HoldingsAdapterConsortium}}
		lookupAdapter, _, err := NewLookupAdapterFactory(nil, nil, "", sources, creator).GetAdapterRequester(appCtx, broken)
		require.NoError(t, err)
		_, err = lookupAdapter.Lookup(catalog.LookupParams{})
		require.ErrorIs(t, err, catalog.ErrLookupAdapterConfig)
		require.ErrorIs(t, err, configErr)
	})

	t.Run("leading config error fails immediately", func(t *testing.T) {
		sources := []catalog.HoldingsSource{{Name: catalog.HoldingsAdapterConsortium}, {Name: catalog.HoldingsAdapterSru, Adapter: &catalog.MockLookupAdapter{}}}
		lookupAdapter, _, err := NewLookupAdapterFactory(nil, nil, "", sources, creator).GetAdapterRequester(appCtx, broken)
		require.Nil(t, lookupAdapter)
		require.ErrorIs(t, err, catalog.ErrLookupAdapterConfig)
		require.ErrorIs(t, err, configErr)
	})
}
