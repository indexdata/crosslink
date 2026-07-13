package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateHoldings(t *testing.T) {
	m := make(map[string]any)

	_, err := CreateLookupAdapterFromEnv(m)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_ADAPTER")

	m[HoldingsAdapter] = "sru"

	_, err = CreateLookupAdapterFromEnv(m)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_SRU_URL")

	m[HoldingsSruURL] = "http://example.com"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_ISXN_LOOKUP")

	m[HoldingsIsxnLookup] = "fake"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "invalid value for HOLDINGS_ISXN_LOOKUP")

	m[HoldingsIsxnLookup] = true
	m[HoldingsFormat] = "reservoir"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.NoError(t, err)

	m[HoldingsFormat] = "MARC-21plus-1"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.NoError(t, err)

	m[HoldingsFormat] = "marc"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.NoError(t, err)

	m[HoldingsFormat] = "opac"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.NoError(t, err)

	m[HoldingsFormat] = "other"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.ErrorContains(t, err, "bad value for HOLDINGS_FORMAT: other")

	m[HoldingsFormat] = true
	_, err = CreateLookupAdapterFromEnv(m)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_FORMAT")

	m[HoldingsAdapter] = "mock"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.NoError(t, err)

	m[HoldingsAdapter] = "other"
	_, err = CreateLookupAdapterFromEnv(m)
	assert.ErrorContains(t, err, "bad value for HOLDINGS_ADAPTER")
}
