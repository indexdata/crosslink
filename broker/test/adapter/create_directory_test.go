package adapter

import (
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCreateDirectoryLookupAdapter(t *testing.T) {
	m := make(map[string]string)

	_, err := adapter.CreateDirectoryLookupAdapter(m)
	assert.ErrorContains(t, err, "missing value for DIRECTORY_ADAPTER")

	m[adapter.DirectoryAdapter] = "api"

	_, err = adapter.CreateDirectoryLookupAdapter(m)
	assert.ErrorContains(t, err, "missing value for DIRECTORY_API_URL")

	m[adapter.DirectoryApiUrl] = "http://example.com"
	_, err = adapter.CreateDirectoryLookupAdapter(m)
	assert.Nil(t, err)

	m["DIRECTORY_ADAPTER"] = "mock"
	_, err = adapter.CreateDirectoryLookupAdapter(m)
	assert.Nil(t, err)

	m["DIRECTORY_ADAPTER"] = "other"
	_, err = adapter.CreateDirectoryLookupAdapter(m)
	assert.ErrorContains(t, err, "bad value for DIRECTORY_ADAPTER")
}
