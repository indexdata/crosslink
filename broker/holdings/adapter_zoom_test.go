//go:build cgo

package holdings

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
	config := directory.MarcParserConfig{
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
	assert.Equal(t, containerHost+":"+mappedPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "20", aa.(*ZoomAvailabilityAdapter).options["count"])

	params := LookupParams{
		Title: "Computer",
	}
	results, pqf, err := aa.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 42)
	assert.Contains(t, results[0].Location, "11224466")
	assert.Equal(t, "@attr 1=1016 \"Computer\"", pqf)
}

func TestLookupFoundOpac(t *testing.T) {
	cqlType := directory.Cql
	queryBuilder, err := NewQueryBuilderGen(&directory.QueryConfig{
		Type: &cqlType,
	})
	assert.NoError(t, err)
	holdingsParser := NewOpacHoldingsParser(directory.OpacParserConfig{})
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
	assert.Equal(t, containerHost+":"+mappedPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "10", aa.(*ZoomAvailabilityAdapter).options["count"])

	params := LookupParams{
		Title: "Computer",
	}
	results, cql, err := aa.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 42)
	assert.Contains(t, results[0].ItemId, "test__000000001_")
	assert.Contains(t, results[1].ItemId, "test__000000002_")
	assert.Equal(t, "title = \"Computer\"", cql)
}

func TestLookupDiagnosticPQF(t *testing.T) {
	queryBuilder, err := NewQueryBuilderGen(nil)
	assert.NoError(t, err)
	holdingsParser := NewMarcHoldingsParser(directory.MarcParserConfig{})
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
	assert.Equal(t, "localhost:"+mappedPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "danmarc", aa.(*ZoomAvailabilityAdapter).options["preferredRecordSyntax"])

	params := LookupParams{Identifier: "1234"}
	_, pqf, err := aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search server with PQF")
	assert.Contains(t, err.Error(), "Record syntax not supported")
	assert.Equal(t, "@attr 1=12 \"1234\"", pqf)
}

func TestLookupDiagnosticCql(t *testing.T) {
	cqlType := directory.Cql
	queryBuilder, err := NewQueryBuilderGen(&directory.QueryConfig{
		Type: &cqlType,
	})
	assert.NoError(t, err)
	holdingsParser := NewMarcHoldingsParser(directory.MarcParserConfig{})
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
	assert.Equal(t, "localhost:"+mappedPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
	assert.Equal(t, "danmarc", aa.(*ZoomAvailabilityAdapter).options["preferredRecordSyntax"])

	params := LookupParams{Identifier: "1234"}
	_, cql, err := aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search server with CQL")
	assert.Contains(t, err.Error(), "Record syntax not supported")
	assert.Equal(t, "rec.id = \"1234\"", cql)
}

func TestConnectFailure(t *testing.T) {
	queryBuilder, err := NewQueryBuilderGen(nil)
	assert.NoError(t, err)
	holdingsParser := NewMarcHoldingsParser(directory.MarcParserConfig{})
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
	_, _, err = aa.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Z39.50 server")
}
