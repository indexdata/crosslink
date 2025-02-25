package adapter

import (
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/stretchr/testify/assert"
)

func TestCreateHoldings(t *testing.T) {
	m := make(map[string]string)

	_, err := adapter.CreateHoldingsLookupAdapter(m)
	assert.ErrorContains(t, err, "missing value for HOLDINGS_ADAPTER")

	m[adapter.HoldingsAdapter] = "sru"

	_, err = adapter.CreateHoldingsLookupAdapter(m)
	assert.ErrorContains(t, err, "missing value for SRU_URL")

	m[adapter.SruUrl] = "http://example.com"
	_, err = adapter.CreateHoldingsLookupAdapter(m)
	assert.Nil(t, err)

	m["HOLDINGS_ADAPTER"] = "mock"
	_, err = adapter.CreateHoldingsLookupAdapter(m)
	assert.Nil(t, err)

	m["HOLDINGS_ADAPTER"] = "other"
	_, err = adapter.CreateHoldingsLookupAdapter(m)
	assert.ErrorContains(t, err, "bad value for HOLDINGS_ADAPTER")
}
