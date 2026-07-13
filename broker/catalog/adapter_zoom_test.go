//go:build cgo

package catalog

import (
	"context"
	"os"
	"testing"

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
	config := directory.MarcHoldingsParserConfig{
		MainField:        NewString("010"),
		LocationSubField: NewString("a"),
	}
	pqfType := directory.Pqf
	queryBuilder, err := NewQueryBuilderGen(&directory.QueryConfig{
		Title: NewString("@attr 1=1016 {term}"),
		Type:  &pqfType,
	})
	assert.NoError(t, err)
	holdingsParser := NewMarcHoldingsParser(config)
	metadataParser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{})
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
		metadataParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, containerHost+":"+mappedPort+"/marc", aa.(*ZoomLookupAdapter).zurl)
	assert.Equal(t, "20", aa.(*ZoomLookupAdapter).options["count"])

	params := LookupParams{
		Title: "Computer",
	}
	result, err := aa.Lookup(params)
	assert.NoError(t, err)

	assert.Equal(t, "@attr 1=1016 \"Computer\"", result.GetQuery())

	results, err := result.GetHoldings()
	assert.NoError(t, err)
	assert.Len(t, results, 42)
	assert.Contains(t, results[0].Location, "11224466")

	metadata, err := result.GetMetadata()
	assert.NoError(t, err)
	assert.Contains(t, metadata.Identifier, "11224466")
	assert.Contains(t, metadata.Title, "How to program a computer")
	assert.Equal(t, "", metadata.Isbn)
	assert.Equal(t, "", metadata.Issn)
}

func TestLookupFoundOpac(t *testing.T) {
	cqlType := directory.Cql
	queryBuilder, err := NewQueryBuilderGen(&directory.QueryConfig{
		Type: &cqlType,
	})
	assert.NoError(t, err)
	holdingsParser := NewOpacHoldingsParser(directory.OpacHoldingsParserConfig{})
	metadataParser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(
		directory.ZoomConfig{
			Address: containerHost + ":" + mappedPort + "/marc",
			Options: &map[string]string{
				"preferredRecordSyntax": "opac",
			},
		},
		queryBuilder,
		holdingsParser,
		metadataParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, containerHost+":"+mappedPort+"/marc", aa.(*ZoomLookupAdapter).zurl)
	assert.Equal(t, "10", aa.(*ZoomLookupAdapter).options["count"])

	params := LookupParams{
		Title: "Computer",
	}
	result, err := aa.Lookup(params)
	assert.NoError(t, err)
	assert.Equal(t, "title = \"Computer\"", result.GetQuery())
	results, err := result.GetHoldings()
	assert.NoError(t, err)
	assert.Len(t, results, 42)
	assert.Contains(t, results[0].ItemId, "test__000000001_")
	assert.Contains(t, results[1].ItemId, "test__000000002_")
	assert.Equal(t, "title = \"Computer\"", result.GetQuery())

	metadata, err := result.GetMetadata()
	assert.NoError(t, err)
	assert.Contains(t, metadata.Identifier, "11224466")
	assert.Contains(t, metadata.Title, "How to program a computer")
}

func TestLookupDiagnosticPQF(t *testing.T) {
	queryBuilder, err := NewQueryBuilderGen(nil)
	assert.NoError(t, err)
	holdingsParser := NewMarcHoldingsParser(directory.MarcHoldingsParserConfig{})
	metadataParser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(
		directory.ZoomConfig{
			Address: containerHost + ":" + mappedPort + "/marc",
			Options: &map[string]string{
				"preferredRecordSyntax": "danmarc",
			},
		},
		queryBuilder,
		holdingsParser,
		metadataParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:"+mappedPort+"/marc", aa.(*ZoomLookupAdapter).zurl)
	assert.Equal(t, "danmarc", aa.(*ZoomLookupAdapter).options["preferredRecordSyntax"])

	params := LookupParams{Identifier: "1234"}
	result, err := aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search server with PQF")
	assert.Contains(t, err.Error(), "Record syntax not supported")
	assert.Equal(t, "@attr 1=12 \"1234\"", result.GetQuery())
}

func TestLookupDiagnosticCql(t *testing.T) {
	cqlType := directory.Cql
	queryBuilder, err := NewQueryBuilderGen(&directory.QueryConfig{
		Type: &cqlType,
	})
	assert.NoError(t, err)
	holdingsParser := NewMarcHoldingsParser(directory.MarcHoldingsParserConfig{})
	metadataParser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(
		directory.ZoomConfig{
			Address: containerHost + ":" + mappedPort + "/marc",
			Options: &map[string]string{
				"preferredRecordSyntax": "danmarc",
			},
		},
		queryBuilder,
		holdingsParser,
		metadataParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:"+mappedPort+"/marc", aa.(*ZoomLookupAdapter).zurl)
	assert.Equal(t, "danmarc", aa.(*ZoomLookupAdapter).options["preferredRecordSyntax"])

	params := LookupParams{Identifier: "1234"}
	result, err := aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search server with CQL")
	assert.Contains(t, err.Error(), "Record syntax not supported")
	assert.Equal(t, "rec.id = \"1234\"", result.GetQuery())
}

func TestConnectFailure(t *testing.T) {
	queryBuilder, err := NewQueryBuilderGen(nil)
	assert.NoError(t, err)
	holdingsParser := NewMarcHoldingsParser(directory.MarcHoldingsParserConfig{})
	metadataParser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{})
	aa, err := NewZoomAvailabilityAdapter(
		directory.ZoomConfig{
			Address: "",
		},
		queryBuilder,
		holdingsParser,
		metadataParser,
	)
	assert.NoError(t, err)
	params := LookupParams{}
	_, err = aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Z39.50 server")
}
