package adapter

import (
	"testing"

	"github.com/indexdata/crosslink/broker/holdings"
	"github.com/stretchr/testify/assert"
)

func TestCreateHoldings(t *testing.T) {
	m := make(map[string]any)

	_, err := holdings.CreateHoldingsLookupShared(m)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_ADAPTER")

	m[holdings.HoldingsAdapter] = "sru"

	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_SRU_URL")

	m[holdings.HoldingsSruURL] = "http://example.com"
	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_ISXN_LOOKUP")

	m[holdings.HoldingsIsxnLookup] = "fake"
	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "invalid value for HOLDINGS_ISXN_LOOKUP")

	m[holdings.HoldingsIsxnLookup] = true
	m[holdings.HoldingsFormat] = "reservoir"
	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.NoError(t, err)

	m[holdings.HoldingsFormat] = "MARC-21plus-1"
	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.NoError(t, err)

	m[holdings.HoldingsFormat] = "other"
	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.ErrorContains(t, err, "bad value for HOLDINGS_FORMAT: other")

	m[holdings.HoldingsFormat] = true
	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_FORMAT")

	m[holdings.HoldingsAdapter] = "mock"
	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.NoError(t, err)

	m[holdings.HoldingsAdapter] = "other"
	_, err = holdings.CreateHoldingsLookupShared(m)
	assert.ErrorContains(t, err, "bad value for HOLDINGS_ADAPTER")
}
