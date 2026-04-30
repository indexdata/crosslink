// file: z3950.go
//go:build cgo

package availability

import (
	"context"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

func TestLookup(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	adapter, err := NewZ3950AvailabilityAdapter(ctx, directory.Z3950Config{
		Address: "z3950.indexdata.com/marc",
	})
	assert.NoError(t, err)

	// existing title
	params := AvailabilityLookupParams{
		Title: "Computer processing of dynamic images from an Anger scintillation camera",
	}
	results, err := adapter.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 1)

	// not-existing title
	params = AvailabilityLookupParams{
		Title: "Art of computer",
	}
	results, err = adapter.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 0)

	// the server does not support searching by ISBN, so this should return an error
	params = AvailabilityLookupParams{
		Isbn: "978-1-56619-909-4",
	}
	results, err = adapter.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search Z39.50 server")
}
