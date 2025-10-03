package ncip

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarshal(t *testing.T) {
	// Just a stub to make sure the generated code compiles
	req := &NCIPMessage{
		LookupUser: &LookupUser{
			InitiationHeader: &InitiationHeader{},
		},
	}
	bytes, err := xml.Marshal(req)
	assert.NoError(t, err)
	assert.Equal(t, `<NCIPMessage version=""><LookupUser><InitiationHeader><FromAgencyId><AgencyId></AgencyId>`+
		`</FromAgencyId><ToAgencyId><AgencyId></AgencyId></ToAgencyId></InitiationHeader></LookupUser></NCIPMessage>`,
		string(bytes))
	t.Logf("XML: %s", bytes)
}
