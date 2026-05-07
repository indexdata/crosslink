// file: z3950.go
//go:build cgo

package availability

import (
	"context"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

func TestLookup(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	// target does not return holdings, we just use 010$a as fake location to verify that the record was parsed correctly
	config := directory.MarcParserConfig{
		MainField:        adapter.NewString("010"),
		LocationSubField: adapter.NewString("a"),
	}
	queryBuilder := adapter.NewQueryBuilderPqf(&directory.QueryConfig{
		Identifier: adapter.NewString("@attr 1=1016 {term}"),
	})
	holdingsParser := adapter.NewMarcHoldingsParser(config)
	aa, err := NewZoomAvailabilityAdapter(ctx,
		directory.Z3950Config{
			Address: "z3950.indexdata.com/marc",
			Options: &map[string]string{
				"count":                 "3",
				"preferredRecordSyntax": "usmarc",
			},
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, "z3950.indexdata.com/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "3", aa.(*ZoomAvailabilityAdapter).options["count"])

	// existing title
	params := adapter.HoldingLookupParams{
		Title: "Computer processing of dynamic images from an Anger scintillation camera",
	}
	results, pqf, err := aa.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Contains(t, results[0].Location, "73090924")
	assert.Equal(t, "@attr 1=4 \"Computer processing of dynamic images from an Anger scintillation camera\"", pqf)

	// not-existing title
	params = adapter.HoldingLookupParams{
		Title: "Art of computer",
	}
	results, pqf, err = aa.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 0)
	assert.Equal(t, "@attr 1=4 \"Art of computer\"", pqf)

	// the server does not support searching by ISBN, so this should return an error
	params = adapter.HoldingLookupParams{
		Isbn: "0836968433",
	}
	_, _, err = aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search Z39.50 server query: @attr 1=7 \"0836968433\"")

	params = adapter.HoldingLookupParams{
		Identifier: "0836968433",
	}
	_, _, err = aa.Lookup(params)
	assert.NoError(t, err)
}

func TestConnectFailure(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)

	config := directory.MarcParserConfig{
		MainField:        adapter.NewString("010"),
		LocationSubField: adapter.NewString("a"),
	}
	queryBuilder := adapter.NewQueryBuilderPqf(&directory.QueryConfig{
		Identifier: adapter.NewString("@attr 1=1016 {term}"),
	})
	holdingsParser := adapter.NewMarcHoldingsParser(config)
	aa, err := NewZoomAvailabilityAdapter(ctx,
		directory.Z3950Config{
			Address: "",
			Options: &map[string]string{
				"count":                 "3",
				"preferredRecordSyntax": "usmarc",
			},
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	params := adapter.HoldingLookupParams{}
	_, _, err = aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Z39.50 server")
}
