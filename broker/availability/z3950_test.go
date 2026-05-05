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
	imap := "@attr 1=1016 {term}"
	adapter, err := NewZ3950AvailabilityAdapter(ctx, directory.Z3950Config{
		Address: "z3950.indexdata.com/marc",
		Options: &map[string]string{
			"count":                 "8",
			"preferredRecordSyntax": "usmarc",
		},
		PqfMappings: &directory.PqfMappings{
			Identifier: &imap,
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, "z3950.indexdata.com/marc", adapter.(*Z3950AvailabilityAdapter).zurl)
	assert.Equal(t, "8", adapter.(*Z3950AvailabilityAdapter).options["count"])

	// existing title
	params := AvailabilityLookupParams{
		Title: "Computer processing of dynamic images from an Anger scintillation camera",
	}
	results, err := adapter.Lookup(params)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Contains(t, results[0].Location, "scintillation")

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
	assert.Contains(t, err.Error(), "failed to search Z39.50 server query: @attr 1=7 \"0836968433\"")

	params = AvailabilityLookupParams{
		Identifier: "0836968433",
	}
	_, err = adapter.Lookup(params)
	assert.NoError(t, err)
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
