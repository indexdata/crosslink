//go:build cgo

package availability

import (
	"context"
	"os"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/directory"
	zoomtestutil "github.com/indexdata/crosslink/testutil"
	"github.com/stretchr/testify/assert"
)

var mappedPort string
var containerHost string

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	metaproxyContainer, err := zoomtestutil.MetaproxyContainerStart(ctx)
	if err != nil {
		panic(err)
	}
	mappedPort = metaproxyContainer.MappedPort()
	containerHost = metaproxyContainer.ContainerHost()

	code := m.Run()

	if metaproxyContainer != nil {
		_ = metaproxyContainer.Terminate(ctx)
	}
	os.Exit(code)
}

func TestLookupFoundMarc(t *testing.T) {
	// target does not return holdings, we just use 010$a as fake location to verify that the record was parsed correctly
	config := directory.MarcParserConfig{
		MainField:        adapter.NewString("010"),
		LocationSubField: adapter.NewString("a"),
	}
	queryBuilder, err := adapter.NewQueryBuilder(&directory.QueryConfig{
		Title: adapter.NewString("@attr 1=1016 {term}"),
	})
	assert.NoError(t, err)
	holdingsParser := adapter.NewMarcHoldingsParser(config)
	aa, err := NewZoomAvailabilityAdapter(
		directory.ZoomConfig{
			Address: containerHost + ":" + mappedPort + "/marc",
			Options: &map[string]string{
				"count":                 "20",
				"preferredRecordSyntax": "usmarc",
			},
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, containerHost+":"+mappedPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "20", aa.(*ZoomAvailabilityAdapter).options["count"])

	params := adapter.LookupParams{
		Title: "Computer",
	}
	results, pqf, err := aa.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 42)
	assert.Contains(t, results[0].Location, "11224466")
	assert.Equal(t, "@attr 1=1016 \"Computer\"", pqf)
}

func TestLookupFoundOpac(t *testing.T) {
	queryBuilder, err := adapter.NewQueryBuilder(nil)
	assert.NoError(t, err)
	holdingsParser := adapter.NewOpacHoldingsParser(directory.OpacParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(
		directory.ZoomConfig{
			Address: containerHost + ":" + mappedPort + "/marc",
			Options: &map[string]string{
				"preferredRecordSyntax": "opac",
			},
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, containerHost+":"+mappedPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "10", aa.(*ZoomAvailabilityAdapter).options["count"])

	params := adapter.LookupParams{
		Title: "Computer",
	}
	results, pqf, err := aa.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 42)
	assert.Contains(t, results[0].ItemId, "test__000000001_")
	assert.Contains(t, results[1].ItemId, "test__000000002_")
	assert.Equal(t, "@attr 1=4 \"Computer\"", pqf)
}

func TestLookupDiagnostics(t *testing.T) {
	queryBuilder, err := adapter.NewQueryBuilder(nil)
	assert.NoError(t, err)
	holdingsParser := adapter.NewMarcHoldingsParser(directory.MarcParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(
		directory.ZoomConfig{
			Address: containerHost + ":" + mappedPort + "/marc",
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

	params := adapter.LookupParams{Identifier: "1234"}
	_, pqf, err := aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Record syntax not supported")
	assert.Equal(t, "@attr 1=12 \"1234\"", pqf)
}

func TestConnectFailure(t *testing.T) {
	queryBuilder, err := adapter.NewQueryBuilder(nil)
	assert.NoError(t, err)
	holdingsParser := adapter.NewMarcHoldingsParser(directory.MarcParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(
		directory.ZoomConfig{
			Address: "",
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	params := adapter.LookupParams{}
	_, _, err = aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Z39.50 server")
}
