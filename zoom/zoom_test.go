//go:build cgo

package zoom

import (
	"context"
	"os"
	"testing"

	"github.com/indexdata/crosslink/zoom/testutil"
	"github.com/stretchr/testify/assert"
)

var mappedPort string

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	metaproxyContainer, err := testutil.MetaproxyContainerStart(ctx)
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
	err = conn.Connect("localhost:" + mappedPort)
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

	record.Close()

	record, err = rs.GetRecord(1)
	assert.NoError(t, err)
	assert.Nil(t, record)

	record, err = rs.GetRecord(-1)
	assert.NoError(t, err)
	assert.Nil(t, record)

	rs.Close()

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
	err := conn.Connect("localhost:" + mappedPort)
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
	err := conn.Connect("localhost:" + mappedPort)
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

func TestSearchSurrogateDiagnostic(t *testing.T) {
	options := Options{
		"elementSetName": "SD", // trigger surrogate diagnostic response from the server
	}
	conn := NewConnection(options)
	assert.NotNil(t, conn)
	defer conn.Close()
	err := conn.Connect("localhost:" + mappedPort)
	assert.NoError(t, err)

	rs, err := conn.Search("@attr 1=4 computer")
	assert.NoError(t, err)
	assert.NotNil(t, rs)
	assert.Equal(t, rs.Count(), 42)

	// getting surrogate diagnostic for element set name not supported
	rec, err := rs.GetRecord(0)
	assert.Error(t, err)
	assert.Nil(t, rec)
	assert.Contains(t, err.Error(), "Specified element set name not valid for specified database")
	assert.Equal(t, 25, err.(*ZoomError).Code)
}
