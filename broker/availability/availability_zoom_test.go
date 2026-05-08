//go:build cgo

package availability

import (
	"context"
	"os"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/zoom"
	"github.com/stretchr/testify/assert"
)

var mappedPort string

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	metaproxyContainer, err := zoom.MetaproxyContainerStart(ctx)
	if err != nil {
		panic(err)
	}
	mappedPort = metaproxyContainer.MappedPort()
	code := m.Run()

	if metaproxyContainer != nil {
		_ = metaproxyContainer.Terminate(ctx)
	}
	os.Exit(code)
}

func TestLookupFound(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	// target does not return holdings, we just use 010$a as fake location to verify that the record was parsed correctly
	config := directory.MarcParserConfig{
		MainField:        adapter.NewString("010"),
		LocationSubField: adapter.NewString("a"),
	}
	queryBuilder := adapter.NewQueryBuilderPqf(&directory.QueryConfig{
		Title: adapter.NewString("@attr 1=1016 {term}"),
	})
	holdingsParser := adapter.NewMarcHoldingsParser(config)
	aa, err := NewZoomAvailabilityAdapter(ctx,
		directory.Z3950Config{
			Address: "localhost:" + mappedPort + "/marc",
			Options: &map[string]string{
				"count":                 "20",
				"preferredRecordSyntax": "usmarc",
			},
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:"+mappedPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "20", aa.(*ZoomAvailabilityAdapter).options["count"])

	params := adapter.HoldingLookupParams{
		Title: "Computer",
	}
	results, pqf, err := aa.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 42)
	assert.Contains(t, results[0].Location, "11224466")
	assert.Equal(t, "@attr 1=1016 \"Computer\"", pqf)
}

func TestLookupDiagnostics(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	queryBuilder := adapter.NewQueryBuilderPqf(nil)
	holdingsParser := adapter.NewMarcHoldingsParser(directory.MarcParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(ctx,
		directory.Z3950Config{
			Address: "localhost:" + mappedPort + "/marc",
			Options: &map[string]string{
				"preferredRecordSyntax": "danmarc",
			},
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:"+mappedPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "danmarc", aa.(*ZoomAvailabilityAdapter).options["preferredRecordSyntax"])

	params := adapter.HoldingLookupParams{Identifier: "1234"}
	_, pqf, err := aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Record syntax not supported")
	assert.Equal(t, "@attr 1=12 \"1234\"", pqf)
}

func TestConnectFailure(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	queryBuilder := adapter.NewQueryBuilderPqf(nil)
	holdingsParser := adapter.NewMarcHoldingsParser(directory.MarcParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(ctx,
		directory.Z3950Config{
			Address: "",
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
