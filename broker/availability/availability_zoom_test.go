// file: z3950.go
//go:build cgo

package availability

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	testMetaproxyContainer testcontainers.Container
	metaproxyHostPort      string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	hostPath, err := filepath.Abs("../../zoom/backend_test.xml")
	if err != nil {
		panic(err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "ghcr.io/indexdata/metaproxy:sha-475f9b5",
		ExposedPorts: []string{"9000/tcp"},
		WaitingFor:   wait.ForListeningPort("9000/tcp").WithStartupTimeout(5 * time.Second),
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      hostPath,
				ContainerFilePath: "/etc/metaproxy/filters-enabled/backend_test.xml",
				FileMode:          0444, // Read-only
			},
		},
	}

	testMetaproxyContainer, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		panic(err)
	}

	// Get the mapped host port for 9000/tcp
	mappedPort, err := testMetaproxyContainer.MappedPort(ctx, "9000/tcp")
	if err != nil {
		_ = testMetaproxyContainer.Terminate(ctx)
		panic(err)
	}
	metaproxyHostPort = mappedPort.Port()

	code := m.Run()

	if testMetaproxyContainer != nil {
		_ = testMetaproxyContainer.Terminate(ctx)
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
			Address: "localhost:" + metaproxyHostPort + "/marc",
			Options: &map[string]string{
				"count":                 "20",
				"preferredRecordSyntax": "usmarc",
			},
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:"+metaproxyHostPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
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
			Address: "localhost:" + metaproxyHostPort + "/marc",
			Options: &map[string]string{
				"preferredRecordSyntax": "danmarc",
			},
		},
		queryBuilder,
		holdingsParser,
	)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:"+metaproxyHostPort+"/marc", aa.(*ZoomAvailabilityAdapter).zurl)
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
