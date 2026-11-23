package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type countingLookup struct {
	MockLookupAdapter
	calls int
}

func (c *countingLookup) Lookup(params LookupParams) (LookupResult, error) {
	c.calls++
	return c.MockLookupAdapter.Lookup(params)
}

type nilLookup struct{}

func (nilLookup) Lookup(LookupParams) (LookupResult, error) { return nil, nil }

func chainOf(adapters ...LookupAdapter) *ChainLookupAdapter {
	entries := make([]ChainEntry, len(adapters))
	for i, a := range adapters {
		entries[i] = ChainEntry{Source: []string{ConfigurationSourceDirectory, ConfigurationSourceGlobal, "third"}[i], Adapter: a}
	}
	return NewChainLookupAdapter(entries)
}

func lookupSource(t *testing.T, result LookupResult) string {
	t.Helper()
	sourced, ok := result.(SourcedLookupResult)
	require.True(t, ok)
	return sourced.ConfigurationSource()
}

func TestChainLookupAdapter(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	holdingsErr := errors.New("holdings failed")
	holding := []Holding{{Symbol: "ISIL:SECOND"}}

	t.Run("first with holdings wins", func(t *testing.T) {
		first := &countingLookup{MockLookupAdapter: MockLookupAdapter{Holdings: []Holding{{Symbol: "ISIL:FIRST"}}}}
		second := &countingLookup{MockLookupAdapter: MockLookupAdapter{Holdings: holding}}
		result, err := chainOf(first, second).Lookup(LookupParams{})
		require.NoError(t, err)
		holdings, err := result.GetHoldings()
		require.NoError(t, err)
		require.Equal(t, "ISIL:FIRST", holdings[0].Symbol)
		require.Equal(t, ConfigurationSourceDirectory, lookupSource(t, result))
		require.Equal(t, 0, second.calls)
	})

	t.Run("empty result falls through", func(t *testing.T) {
		result, err := chainOf(&MockLookupAdapter{}, &MockLookupAdapter{Holdings: holding}).Lookup(LookupParams{})
		require.NoError(t, err)
		holdings, err := result.GetHoldings()
		require.NoError(t, err)
		require.Equal(t, holding, holdings)
		require.Equal(t, ConfigurationSourceGlobal, lookupSource(t, result))
	})

	t.Run("lookup error stops", func(t *testing.T) {
		second := &countingLookup{MockLookupAdapter: MockLookupAdapter{Holdings: holding}}
		_, err := chainOf(&MockLookupAdapter{Err: lookupErr}, second).Lookup(LookupParams{})
		require.ErrorIs(t, err, lookupErr)
		require.Equal(t, 0, second.calls)
	})

	t.Run("holdings error stops", func(t *testing.T) {
		second := &countingLookup{MockLookupAdapter: MockLookupAdapter{Holdings: holding}}
		result, err := chainOf(&MockLookupAdapter{HoldingsErr: holdingsErr}, second).Lookup(LookupParams{})
		require.NoError(t, err)
		_, err = result.GetHoldings()
		require.ErrorIs(t, err, holdingsErr)
		require.Equal(t, 0, second.calls)
	})

	t.Run("no holdings returns first record", func(t *testing.T) {
		first := &MockLookupAdapter{Metadata: Metadata{Title: "first"}}
		second := &MockLookupAdapter{Metadata: Metadata{Title: "second"}}
		result, err := chainOf(&MockLookupAdapter{}, first, second).Lookup(LookupParams{})
		require.NoError(t, err)
		require.True(t, result.HasRecord())
		metadata, err := result.GetMetadata()
		require.NoError(t, err)
		require.Equal(t, "first", metadata.Title)
		require.Equal(t, ConfigurationSourceGlobal, lookupSource(t, result))
	})

	t.Run("no record returns last result", func(t *testing.T) {
		result, err := chainOf(&MockLookupAdapter{}, &MockLookupAdapter{}).Lookup(LookupParams{})
		require.NoError(t, err)
		require.False(t, result.HasRecord())
		require.Equal(t, ConfigurationSourceGlobal, lookupSource(t, result))
	})

	t.Run("missing lookup parameters falls through", func(t *testing.T) {
		missing := &MockLookupAdapter{Err: newMissingLookupParametersError("no isbn")}
		result, err := chainOf(missing, &MockLookupAdapter{Holdings: holding}).Lookup(LookupParams{})
		require.NoError(t, err)
		holdings, err := result.GetHoldings()
		require.NoError(t, err)
		require.Equal(t, holding, holdings)
	})

	t.Run("missing lookup parameters everywhere is reported", func(t *testing.T) {
		missing := &MockLookupAdapter{Err: newMissingLookupParametersError("no isbn")}
		result, err := chainOf(missing, missing).Lookup(LookupParams{})
		require.ErrorIs(t, err, ErrMissingLookupParameters)
		require.Nil(t, result)
	})

	t.Run("missing lookup parameters after empty result returns it", func(t *testing.T) {
		missing := &MockLookupAdapter{Err: newMissingLookupParametersError("no isbn")}
		result, err := chainOf(&MockLookupAdapter{}, missing).Lookup(LookupParams{})
		require.NoError(t, err)
		require.False(t, result.HasRecord())
		require.Equal(t, ConfigurationSourceDirectory, lookupSource(t, result))
	})

	t.Run("later lookup error keeps earlier record", func(t *testing.T) {
		result, err := chainOf(&MockLookupAdapter{Metadata: Metadata{Title: "first"}}, &MockLookupAdapter{Err: lookupErr}).Lookup(LookupParams{})
		require.NoError(t, err)
		metadata, err := result.GetMetadata()
		require.NoError(t, err)
		require.Equal(t, "first", metadata.Title)
		require.Equal(t, ConfigurationSourceDirectory, lookupSource(t, result))
	})

	t.Run("later holdings error keeps earlier record", func(t *testing.T) {
		result, err := chainOf(&MockLookupAdapter{RecordFound: true}, &MockLookupAdapter{HoldingsErr: holdingsErr}).Lookup(LookupParams{})
		require.NoError(t, err)
		holdings, err := result.GetHoldings()
		require.NoError(t, err)
		require.Empty(t, holdings)
		require.Equal(t, ConfigurationSourceDirectory, lookupSource(t, result))
	})

	t.Run("later error without earlier record stops", func(t *testing.T) {
		_, err := chainOf(&MockLookupAdapter{}, &MockLookupAdapter{Err: lookupErr}).Lookup(LookupParams{})
		require.ErrorIs(t, err, lookupErr)
	})

	t.Run("later nil result keeps earlier result", func(t *testing.T) {
		result, err := chainOf(&MockLookupAdapter{}, nilLookup{}).Lookup(LookupParams{})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, ConfigurationSourceDirectory, lookupSource(t, result))
	})

	t.Run("cancelled context stops", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		first := &countingLookup{}
		_, err := LookupContext(ctx, chainOf(first), LookupParams{})
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 0, first.calls)
	})
}
