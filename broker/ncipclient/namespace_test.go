package ncipclient

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/require"
)

func TestNamespaceFreeExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NotContains(t, string(data), "xmlns")
		require.Contains(t, string(data), "patron&amp;1")
		decoder := xml.NewDecoder(strings.NewReader(string(data)))
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if el, ok := token.(xml.StartElement); ok {
				require.Empty(t, el.Name.Space)
			}
		}
		_, err = w.Write([]byte(`<NCIPMessage><LookupUserResponse><UserId><UserIdentifierValue>patron&amp;1</UserIdentifierValue></UserId></LookupUserResponse></NCIPMessage>`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := NewNcipClient(server.Client(), server.URL, "agency", "", "").(*NcipClientImpl)
	client.DisableNamespace = true
	resp, err := client.LookupUser(ncip.LookupUser{UserId: &ncip.UserId{UserIdentifierValue: "patron&1"}})
	require.NoError(t, err)
	require.Equal(t, "patron&1", resp.UserId.UserIdentifierValue)
}
