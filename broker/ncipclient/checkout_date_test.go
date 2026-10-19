package ncipclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckoutDiscardsUnusableDates(t *testing.T) {
	for _, withoutNamespace := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			body string
			date bool
			fail bool
		}{
			{name: "invalid", body: "<DateDue>invalid</DateDue>"},
			{name: "zero", body: "<DateDue>0001-01-01T00:00:00Z</DateDue>"},
			{name: "year zero", body: "<DateDue>0000-01-01T00:00:00Z</DateDue>"},
			{name: "date only", body: "<DateDue>2026-09-16</DateDue>"},
			{name: "empty", body: "<DateDue></DateDue>"},
			{name: "omitted"},
			{name: "valid", body: "<DateDue>2026-10-01T23:59:59+02:00</DateDue>", date: true},
			{name: "malformed XML", body: "<DateDue>invalid", fail: true},
			{name: "problem", body: "<DateDue>invalid</DateDue><Problem><ProblemType>Unknown Item</ProblemType></Problem>", fail: true},
		} {
			t.Run(tc.name+map[bool]string{false: "/namespaced", true: "/namespace-free"}[withoutNamespace], func(t *testing.T) {
				namespace := ` xmlns="http://www.niso.org/2008/ncip"`
				if withoutNamespace {
					namespace = ""
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/xml")
					_, _ = io.WriteString(w, `<NCIPMessage`+namespace+`><CheckOutItemResponse><ItemId><ItemIdentifierValue>item</ItemIdentifierValue></ItemId>`+tc.body+`</CheckOutItemResponse></NCIPMessage>`)
				}))
				defer server.Close()
				client := NewNcipClient(server.Client(), server.URL, "from", "to", "", withoutNamespace)
				response, err := client.CheckOutItem(ncip.CheckOutItem{})
				if tc.fail {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.NotNil(t, response)
				require.NotNil(t, response.ItemId)
				assert.Equal(t, "item", response.ItemId.ItemIdentifierValue)
				if tc.date {
					require.NotNil(t, response.DateDue)
					assert.Equal(t, "2026-10-01T23:59:59+02:00", response.DateDue.Format("2006-01-02T15:04:05Z07:00"))
				} else {
					assert.Nil(t, response.DateDue)
				}
			})
		}
	}
}

func TestGenericDecoderPreservesXSDDateHandling(t *testing.T) {
	client := &NcipClientImpl{}
	var response ncip.NCIPMessage
	err := client.unmarshal([]byte(`<NCIPMessage xmlns="http://www.niso.org/2008/ncip"><CheckOutItemResponse><DateDue>invalid</DateDue></CheckOutItemResponse></NCIPMessage>`), &response)
	require.NoError(t, err)
	require.NotNil(t, response.CheckOutItemResponse)
	require.NotNil(t, response.CheckOutItemResponse.DateDue)
	assert.True(t, response.CheckOutItemResponse.DateDue.IsZero())
}
