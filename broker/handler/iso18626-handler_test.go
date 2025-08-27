package handler

import (
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestGetSupplierSymbol(t *testing.T) {
	header := &iso18626.Header{
		SupplyingAgencyId: iso18626.TypeAgencyId{
			AgencyIdType: iso18626.TypeSchemeValuePair{
				Text: "ISIL",
			},
			AgencyIdValue: "12345",
		},
	}
	symbol := getSupplierSymbol(header)
	assert.Equal(t, "ISIL:12345", symbol)
	header.SupplyingAgencyId.AgencyIdType.Text = ""
	symbol = getSupplierSymbol(header)
	assert.Equal(t, "", symbol)
	header.SupplyingAgencyId.AgencyIdType.Text = "ISIL"
	header.SupplyingAgencyId.AgencyIdValue = ""
	symbol = getSupplierSymbol(header)
	assert.Equal(t, "", symbol)
}
