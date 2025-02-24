package adapter

import (
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/stretchr/testify/assert"
)

func TestCreateHoldings(t *testing.T) {
	_, err := adapter.CreateHoldings("sru", "http://example.com")
	assert.Nil(t, err)

	_, err = adapter.CreateHoldings("mock", "http://example.com")
	assert.Nil(t, err)

	_, err = adapter.CreateHoldings("other", "http://example.com")
	assert.ErrorContains(t, err, "bad value for HOLDINGS_ADAPTER")
}
