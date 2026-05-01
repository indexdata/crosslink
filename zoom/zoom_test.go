// file: zoom_test.go
//go:build cgo

package zoom

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
	}

	conn = NewConnection(options)
	assert.NotNil(t, conn)
	err = conn.Connect("z3950.indexdata.com/marc")
	assert.NoError(t, err)

	rs, err := conn.Search("@attr 1=4 computer")
	assert.NoError(t, err)
	assert.NotNil(t, rs)
	assert.Greater(t, rs.Count(), 7)

	rs, err = conn.Search("@attr 1=4 program")
	assert.NoError(t, err)
	assert.NotNil(t, rs)
	assert.Greater(t, rs.Count(), 2)

	record, err := rs.GetRecord(0)
	assert.NoError(t, err)
	assert.NotNil(t, record)
	assert.Contains(t, record.Data("render"), "program")
	assert.Equal(t, "", record.Data("unknown"))

	record.finalize()

	record, err = rs.GetRecord(-1)
	assert.NoError(t, err)
	assert.Nil(t, record)

	rs.finalize()

	_, err = rs.GetRecord(0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "result set is not available")

	conn.Close()
	_, err = conn.Search("@attr 1=4 computer")
	assert.NoError(t, err)

	conn.finalize()
	_, err = conn.Search("@attr 1=4 computer")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection is not established")
	conn.Close()
}

func TestRecordData(t *testing.T) {
	record := &Record{}
	assert.Equal(t, "", record.Data("render"))
}

func TestSearchUnsupportedSyntax(t *testing.T) {
	options := Options{
		"preferredRecordSyntax": "danmarc", // not supported by the server
	}
	conn := NewConnection(options)
	assert.NotNil(t, conn)
	defer conn.finalize()
	err := conn.Connect("z3950.indexdata.com/marc")
	assert.NoError(t, err)

	rs, err := conn.Search("@attr 1=4 computer")
	assert.NoError(t, err)
	assert.NotNil(t, rs)
	assert.Greater(t, rs.Count(), 7)

	// getting surrogate diagnostic for unsupported record syntax when trying to access the record
	_, err = rs.GetRecord(0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Record not available in requested syntax")
	assert.Equal(t, 238, err.(*ZoomError).Code)
}

func TestSearchUnsupportedAttribute(t *testing.T) {
	conn := NewConnection(Options{})
	assert.NotNil(t, conn)
	defer conn.finalize()
	err := conn.Connect("z3950.indexdata.com/marc")
	assert.NoError(t, err)

	_, err = conn.Search("@attr 1=99 computer")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported Use attribute (99)")
	assert.Equal(t, "99", err.(*ZoomError).AdditionalInfo)
	assert.Equal(t, 114, err.(*ZoomError).Code)
}
