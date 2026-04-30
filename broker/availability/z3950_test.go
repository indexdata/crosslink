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

func TestOptions(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	_, err := NewZ3950AvailabilityAdapter(ctx, directory.Z3950Config{
		Options: &map[string]interface{}{
			"otherOption": false,
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type for option otherOption: expected string")
}

func TestLookup(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	adapter, err := NewZ3950AvailabilityAdapter(ctx, directory.Z3950Config{
		Address: "z3950.indexdata.com/marc",
		Options: &map[string]interface{}{
			"count":                 "8",
			"preferredRecordSyntax": "usmarc",
		},
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
		Isbn: "0836968433",
	}
	_, err = adapter.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to search Z39.50 server")
}

func TestConnectFailure(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	adapter, err := NewZ3950AvailabilityAdapter(ctx, directory.Z3950Config{})
	assert.NoError(t, err)

	params := AvailabilityLookupParams{}
	_, err = adapter.Lookup(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Z39.50 server")
}

func TestPqfEncode(t *testing.T) {
	input := `Special characters: + - & | ! ( ) { } [ ] ^ " ~ * ? : \`
	expected := `"Special characters: + - & | ! ( ) { } [ ] ^ \" ~ * ? : \\"`
	encoded := pqfEncode(input)
	assert.Equal(t, expected, encoded)
}
