package ncip

import (
	"encoding/xml"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarshal(t *testing.T) {
	// Just a stub to make sure the generated code compiles
	req := &NCIPMessage{
		Version: "2.0",
		LookupUser: &LookupUser{
			InitiationHeader: &InitiationHeader{},
		},
	}
	var err error
	exp, err := os.ReadFile("testdata/lookup_user_request.xml")
	assert.NoError(t, err)
	got, err := xml.MarshalIndent(req, "", "  ")
	assert.NoError(t, err)
	assert.Equal(t, string(exp), string(got))
}
