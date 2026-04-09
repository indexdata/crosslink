package ncip

import (
	"encoding/xml"
	"os"
	"testing"
	"time"

	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
)

func TestMarshal(t *testing.T) {
	// Just a stub to make sure the generated code compiles
	urn := "urn:any"
	sampleTime := time.Date(2025, 10, 6, 11, 14, 0, 592000000, time.UTC)
	req := &NCIPMessage{
		Version: NCIP_V2_02_XSD,
		AcceptItem: &AcceptItem{
			InitiationHeader: &InitiationHeader{
				ToAgencyId: ToAgencyId{
					AgencyId: SchemeValuePair{Scheme: &urn, Text: "toAgency"},
				},
			},
			DateForReturn: &utils.XSDDateTime{Time: sampleTime},
		},
	}
	var err error
	exp, err := os.ReadFile("testdata/ncip_sample.xml")
	assert.NoError(t, err)
	got, err := xml.MarshalIndent(req, "", "  ")
	assert.NoError(t, err)
	assert.Equal(t, string(exp), string(got)+"\n")
}
