package ncipclient

import (
	"encoding/xml"
	"testing"

	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnusableCheckoutDateDoesNotDiscardSuccessfulCheckout(t *testing.T) {
	for _, date := range []string{"invalid", "0001-01-01T00:00:00Z", "2026-09-16", ""} {
		input := []byte(`<NCIPMessage xmlns="http://www.niso.org/2008/ncip"><CheckOutItemResponse><ItemId><ItemIdentifierValue>item</ItemIdentifierValue></ItemId><DateDue>` + date + `</DateDue></CheckOutItemResponse></NCIPMessage>`)
		cleaned, err := stripInvalidCheckoutDueDates(input)
		require.NoError(t, err)
		var response ncip.NCIPMessage
		require.NoError(t, xml.Unmarshal(cleaned, &response))
		require.NotNil(t, response.CheckOutItemResponse)
		assert.Nil(t, response.CheckOutItemResponse.DateDue)
		assert.NotNil(t, response.CheckOutItemResponse.ItemId)
	}
	valid := []byte(`<NCIPMessage><CheckOutItemResponse><DateDue>2026-10-01T23:59:59+02:00</DateDue></CheckOutItemResponse></NCIPMessage>`)
	cleaned, err := stripInvalidCheckoutDueDates(valid)
	require.NoError(t, err)
	assert.Equal(t, valid, cleaned)
	_, err = stripInvalidCheckoutDueDates([]byte(`<NCIPMessage><CheckOutItemResponse><DateDue>bad`))
	require.Error(t, err, "malformed XML must still fail")
}
