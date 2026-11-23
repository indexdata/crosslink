package catalog

import (
	"context"
	"errors"
)

// Configuration sources reported for holdings lookups.
const (
	// ConfigurationSourceGlobal identifies an adapter shared by all requesters.
	ConfigurationSourceGlobal string = "global"
	// ConfigurationSourceDirectory identifies an adapter built from a peer's Directory configuration.
	ConfigurationSourceDirectory string = "directory"
)

// ChainEntry is one adapter tried by a ChainLookupAdapter, labelled with its configuration source.
type ChainEntry struct {
	Source  string
	Adapter LookupAdapter
}

// ChainLookupAdapter tries its adapters in order and returns the first result
// with holdings. An adapter that cannot use the lookup parameters
// (ErrMissingLookupParameters) or returns no holdings is skipped. Other lookup
// and holdings errors stop the chain, unless an earlier adapter already found a
// record, which is then returned instead. When no adapter returns holdings, the
// first result with a record is returned (so metadata consumers still get a
// record), otherwise the last non-nil result.
type ChainLookupAdapter struct {
	entries []ChainEntry
}

// NewChainLookupAdapter creates a chain over entries, which must contain non-nil adapters.
func NewChainLookupAdapter(entries []ChainEntry) *ChainLookupAdapter {
	return &ChainLookupAdapter{entries: entries}
}

// SourcedLookupResult is implemented by results that know which configuration source produced them.
type SourcedLookupResult interface {
	LookupResult
	ConfigurationSource() string
}

type chainLookupResult struct {
	LookupResult
	source string
}

func (r *chainLookupResult) ConfigurationSource() string {
	return r.source
}

func (c *ChainLookupAdapter) Lookup(params LookupParams) (LookupResult, error) {
	return c.LookupContext(context.Background(), params)
}

// LookupContext honors cancellation between adapters and passes ctx to context-aware adapters.
func (c *ChainLookupAdapter) LookupContext(ctx context.Context, params LookupParams) (LookupResult, error) {
	var withRecord, last LookupResult
	var missingParamsErr error
	for _, entry := range c.entries {
		result, err := LookupContext(ctx, entry.Adapter, params)
		sourced := wrapChainResult(result, entry.Source)
		if errors.Is(err, ErrMissingLookupParameters) {
			// this source cannot search with these parameters; another may
			if missingParamsErr == nil {
				missingParamsErr = err
			}
			continue
		}
		if err != nil {
			if withRecord != nil && ctx.Err() == nil {
				return withRecord, nil
			}
			return sourced, err
		}
		if result == nil {
			continue
		}
		last = sourced
		holdings, err := result.GetHoldings()
		if err != nil {
			if withRecord != nil {
				return withRecord, nil
			}
			// callers read the holdings error again via GetHoldings
			return sourced, nil
		}
		if len(holdings) > 0 {
			return sourced, nil
		}
		if withRecord == nil && result.HasRecord() {
			withRecord = sourced
		}
	}
	if withRecord != nil {
		return withRecord, nil
	}
	if last == nil && missingParamsErr != nil {
		return nil, missingParamsErr
	}
	return last, nil
}

func wrapChainResult(result LookupResult, source string) LookupResult {
	if result == nil {
		return nil
	}
	return &chainLookupResult{LookupResult: result, source: source}
}
