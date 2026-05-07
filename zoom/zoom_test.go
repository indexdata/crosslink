// file: zoom_test.go
//go:build cgo

package zoom

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	hostPath, err := filepath.Abs("backend_test.xml")
	if err != nil {
		panic(err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "ghcr.io/indexdata/metaproxy:sha-475f9b5",
		ExposedPorts: []string{"9000/tcp"},
		WaitingFor:   wait.ForListeningPort("9000/tcp").WithStartupTimeout(60 * time.Second),
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(hostPath, "/etc/metaproxy/filters-enabled/backend_test.xml"),
		),
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

func TestConnection(t *testing.T) {
	options := Options{
		"databaseName": "testdb",
		"username":     "testuser",
		"password":     "testpass",
	}
	conn := NewConnection(options)
	assert.NotNil(t, conn)
	conn.Close()
}

func TestConnect(t *testing.T) {
	conn := &Connection{}
	err := conn.Connect("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection is not established")

	conn = NewConnection(Options{})
	assert.NotNil(t, conn)
	err = conn.Connect("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Connect failed")
	assert.Equal(t, 10000, err.(*ZoomError).Code)
}

func TestSearch(t *testing.T) {
	conn := &Connection{}
	_, err := conn.Search("@attr 1=4 utah")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection is not established")

	options := Options{
		"preferredRecordSyntax": "usmarc",
		"count":                 "0",
	}

	conn = NewConnection(options)
	assert.NotNil(t, conn)
	err = conn.Connect("localhost:" + metaproxyHostPort)
	assert.NoError(t, err)

	rs, err := conn.Search("@attr 1=4 computer")
	assert.NoError(t, err)
	assert.NotNil(t, rs)
	assert.Equal(t, rs.Count(), 42)

	record, err := rs.GetRecord(0)
	assert.NoError(t, err)
	assert.NotNil(t, record)
	assert.Contains(t, string(record.Data("render")), "How to program a computer")
	assert.Nil(t, record.Data("unknown"))

	conn.Close()
	record, err = rs.GetRecord(0)
	assert.NoError(t, err)
	assert.NotNil(t, record)

	record.finalize()

	record, err = rs.GetRecord(1)
	assert.NoError(t, err)
	assert.Nil(t, record)

	record, err = rs.GetRecord(-1)
	assert.NoError(t, err)
	assert.Nil(t, record)

	rs.finalize()

	_, err = rs.GetRecord(0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "result set is not available")
}

func TestRecordData(t *testing.T) {
	record := &Record{}
	assert.Nil(t, record.Data("render"))
}

func TestSearchUnsupportedSyntaxOnSearch(t *testing.T) {
	options := Options{
		"preferredRecordSyntax": "danmarc", // not supported by the server
		"count":                 "1",
	}
	conn := NewConnection(options)
	assert.NotNil(t, conn)
	defer conn.Close()
	err := conn.Connect("localhost:" + metaproxyHostPort)
	assert.NoError(t, err)

	// getting non-surrogate diagnostic for unsupported record syntax
	rs, err := conn.Search("@attr 1=4 computer")
	assert.Error(t, err)
	assert.Nil(t, rs)
	assert.Contains(t, err.Error(), "Record syntax not supported")
	assert.Equal(t, 239, err.(*ZoomError).Code)
}

func TestSearchUnsupportedSyntaxOnPresent(t *testing.T) {
	options := Options{
		"preferredRecordSyntax": "danmarc", // not supported by the server
		"count":                 "0",
	}
	conn := NewConnection(options)
	assert.NotNil(t, conn)
	defer conn.Close()
	err := conn.Connect("localhost:" + metaproxyHostPort)
	assert.NoError(t, err)

	rs, err := conn.Search("@attr 1=4 computer")
	assert.NoError(t, err)
	assert.NotNil(t, rs)
	assert.Equal(t, rs.Count(), 42)

	// getting non-surrogate diagnostic for unsupported record syntax
	rec, err := rs.GetRecord(0)
	assert.Error(t, err)
	assert.Nil(t, rec)
	assert.Contains(t, err.Error(), "Record syntax not supported")
	assert.Equal(t, 239, err.(*ZoomError).Code)
}
