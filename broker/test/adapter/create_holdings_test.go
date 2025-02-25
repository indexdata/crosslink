package adapter

import (
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/stretchr/testify/assert"
)

func TestCreateHoldings(t *testing.T) {
	_, err := adapter.CreateHoldingsLookupAdapter("sru", "http://example.com")
	assert.Nil(t, err)

	_, err = adapter.CreateHoldingsLookupAdapter("mock", "http://example.com")
	assert.Nil(t, err)

	_, err = adapter.CreateHoldingsLookupAdapter("other", "http://example.com")
	assert.ErrorContains(t, err, "bad value for HOLDINGS_ADAPTER")
}
