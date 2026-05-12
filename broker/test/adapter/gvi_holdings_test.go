package adapter

import (
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/availability"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

func TestGviHoldings(t *testing.T) {
	creator := availability.NewAvailabilityCreator(availability.AvailabilityAdapterZoom, "")

	// TODO: should use mock rather than external service
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Zoom: &directory.ZoomConfig{
					Address: "https://sru.kobv.de/holdingsgvi",
					Options: &map[string]string{
						"sru":         "get",
						"sru_version": "1.1",
					},
				},
				QueryConfig: &directory.QueryConfig{
					Type:       new(directory.Cql),
					Identifier: adapter.NewString("rec.id = {term}"),
				},
				ParserConfig: &directory.ParserConfig{
					Marc21plus1: &map[string]interface{}{},
				},
			},
		},
	}

	aa, err := creator.GetAdapter(peer)
	assert.NoError(t, err)
	assert.NotNil(t, aa)

	params := adapter.LookupParams{
		ServiceType: "Loan",
		Identifier:  "(DE-602)almafu_BV010733623",
	}
	holdings, _, err := aa.Lookup(params)
	assert.NoError(t, err)
	assert.NotNil(t, holdings)
	assert.Len(t, holdings, 85)
}
