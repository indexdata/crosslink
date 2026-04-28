package zoom

import (
	"strconv"
	"testing"

	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"

	test "github.com/indexdata/crosslink/broker/test/utils"
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
	mockPort := utils.Must(test.GetFreePort())

	conn := NewConnection(Options{})
	assert.NotNil(t, conn)
	err := conn.Connect("localhost:" + strconv.Itoa(mockPort))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
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

	_, err = conn.Search("@attr 1=99 program")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "search failed: Unsupported Use attribute")

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

	record, err = rs.GetRecord(-1)
	assert.NoError(t, err)
	assert.Nil(t, record)

	conn.Close()
	_, err = conn.Search("@attr 1=4 computer")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection is not established")
}

func TestRecordData(t *testing.T) {
	record := &Record{}
	assert.Equal(t, "", record.Data("render"))
}

func TestSearchUnsupportedSyntax(t *testing.T) {
	options := Options{
		"preferredRecordSyntax": "danmarc",
	}

	conn := NewConnection(options)
	assert.NotNil(t, conn)
	err := conn.Connect("z3950.indexdata.com/marc")
	assert.NoError(t, err)

	rs, err := conn.Search("@attr 1=4 computer")
	assert.NoError(t, err)
	assert.NotNil(t, rs)
	assert.Greater(t, rs.Count(), 7)

	// would like to get error for this.
	record, err := rs.GetRecord(0)
	assert.NoError(t, err)
	assert.NotNil(t, record)
}
