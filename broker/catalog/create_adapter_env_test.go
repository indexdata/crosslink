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

func TestCreateHoldingsSourceList(t *testing.T) {
	sruCfg := map[string]any{HoldingsSruURL: "http://example.com", HoldingsIsxnLookup: false, HoldingsFormat: "marc"}

	sources, err := CreateLookupAdapterFromEnv(map[string]any{HoldingsAdapter: "mock"})
	assert.NoError(t, err)
	assert.Len(t, sources, 1)
	assert.Equal(t, HoldingsAdapterMock, sources[0].Name)
	assert.IsType(t, &MockLookupShared{}, sources[0].Adapter)

	sources, err = CreateLookupAdapterFromEnv(map[string]any{HoldingsAdapter: "consortium"})
	assert.NoError(t, err)
	assert.Equal(t, []HoldingsSource{{Name: HoldingsAdapterConsortium}}, sources)

	cfg := map[string]any{HoldingsAdapter: " consortium , sru,mock "}
	for k, v := range sruCfg {
		cfg[k] = v
	}
	sources, err = CreateLookupAdapterFromEnv(cfg)
	assert.NoError(t, err)
	assert.Len(t, sources, 3)
	assert.Equal(t, HoldingsAdapterConsortium, sources[0].Name)
	assert.Nil(t, sources[0].Adapter)
	assert.Equal(t, HoldingsAdapterSru, sources[1].Name)
	assert.IsType(t, &SruLookupAdapter{}, sources[1].Adapter)
	assert.Equal(t, HoldingsAdapterMock, sources[2].Name)

	// SRU settings are only required when sru is listed
	_, err = CreateLookupAdapterFromEnv(map[string]any{HoldingsAdapter: "consortium,mock"})
	assert.NoError(t, err)
	_, err = CreateLookupAdapterFromEnv(map[string]any{HoldingsAdapter: "consortium,sru"})
	assert.ErrorContains(t, err, "missing value for HOLDINGS_SRU_URL")

	for value, msg := range map[string]string{
		"":                 "empty entry",
		"mock,":            "empty entry",
		"mock,,sru":        "empty entry",
		"mock,mock":        "duplicate entry \"mock\"",
		"mock, mock":       "duplicate entry \"mock\"",
		"consortium,other": "bad value for HOLDINGS_ADAPTER: other",
	} {
		_, err = CreateLookupAdapterFromEnv(map[string]any{HoldingsAdapter: value})
		assert.ErrorContains(t, err, msg, value)
	}
}
