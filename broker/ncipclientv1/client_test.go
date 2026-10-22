package ncipclientv1

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	ncip "github.com/indexdata/crosslink/ncipv1"
	"github.com/stretchr/testify/require"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testClient(t *testing.T, body string, check func(string)) *Client {
	t.Helper()
	return New(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/xml", r.Header.Get("Content-Type"))
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(data), `version="`+ncip.NCIP_V1_0_DTD+`"`)
		require.NotContains(t, string(data), "xmlns")
		if check != nil {
			check(string(data))
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/xml; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, "https://lms.example/ncip")
}

func TestServices(t *testing.T) {
	operations := []struct {
		name string
		call func(*Client) error
	}{
		{"LookupUser", func(c *Client) error {
			response, err := c.LookupUser(ncip.LookupUser{})
			if err == nil {
				require.NotNil(t, response)
			}
			return err
		}},
		{"AcceptItem", func(c *Client) error {
			response, err := c.AcceptItem(ncip.AcceptItem{})
			if err == nil {
				require.NotNil(t, response)
			}
			return err
		}},
		{"RequestItem", func(c *Client) error {
			response, err := c.RequestItem(ncip.RequestItem{})
			if err == nil {
				require.NotNil(t, response)
			}
			return err
		}},
		{"CancelRequestItem", func(c *Client) error {
			response, err := c.CancelRequestItem(ncip.CancelRequestItem{})
			if err == nil {
				require.NotNil(t, response)
			}
			return err
		}},
		{"CheckInItem", func(c *Client) error {
			response, err := c.CheckInItem(ncip.CheckInItem{})
			if err == nil {
				require.NotNil(t, response)
			}
			return err
		}},
		{"CheckOutItem", func(c *Client) error {
			response, err := c.CheckOutItem(ncip.CheckOutItem{})
			if err == nil {
				require.NotNil(t, response)
			}
			return err
		}},
	}
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			for _, mode := range []string{"success", "processing", "messaging", "missing", "transport"} {
				t.Run(mode, func(t *testing.T) {
					problem := ""
					switch mode {
					case "processing":
						problem = `<Problem><ProcessingError><ProcessingErrorType><Scheme>local</Scheme><Value>Unknown Item</Value></ProcessingErrorType><ProcessingErrorElement><ElementName>UniqueItemId</ElementName><ProcessingErrorElement><ElementName>ItemIdentifierValue</ElementName><ProcessingErrorValue>missing</ProcessingErrorValue></ProcessingErrorElement></ProcessingErrorElement></ProcessingError></Problem>`
					case "messaging":
						problem = `<Problem><MessagingError><MessagingErrorType><Scheme>local</Scheme><Value>Invalid Message</Value></MessagingErrorType><MessagingErrorElement><ElementName>InitiationHeader</ElementName></MessagingErrorElement></MessagingError></Problem>`
					}
					service := op.name + "Response"
					if mode == "missing" {
						service = "LookupItemResponse"
					}
					body := "<NCIPMessage><" + service + ">" + problem + "</" + service + "></NCIPMessage>"
					c := testClient(t, body, func(request string) { require.Contains(t, request, "<"+op.name+">") })
					sentinel := errors.New("transport failed")
					if mode == "transport" {
						c.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, sentinel })
					}
					logged := 0
					c.SetLogFunc(func(out, in map[string]any, err error) {
						logged++
						require.NotNil(t, out)
						require.NotNil(t, in)
						if mode != "success" {
							require.Error(t, err)
						}
					})
					err := op.call(c)
					require.Equal(t, 1, logged)
					switch mode {
					case "success":
						require.NoError(t, err)
					case "transport":
						require.ErrorIs(t, err, sentinel)
					case "missing":
						require.ErrorContains(t, err, "missing "+op.name+"Response")
					default:
						var p *Error
						require.ErrorAs(t, err, &p)
						require.Contains(t, p.Error(), op.name)
						if mode == "processing" {
							require.Contains(t, p.Error(), "Unknown Item")
							require.Equal(t, "missing", p.Problem.ProcessingError[0].ProcessingErrorElement.ProcessingErrorElement.ProcessingErrorValue)
						} else {
							require.Contains(t, p.Error(), "Invalid Message")
						}
					}
				})
			}
		})
	}
}
func TestDecode(t *testing.T) {
	for _, body := range []string{
		"", `<other/>`, `<NCIPMessage/>`, `<NCIPMessage><LookupUserResponse/><CheckInItemResponse/></NCIPMessage>`,
		`<NCIPMessage xmlns="urn:v2"><LookupUserResponse/></NCIPMessage>`,
		`<NCIPMessage><LookupUserResponse xmlns="urn:v2"/></NCIPMessage>`,
		`<NCIPMessage version="2.02"><LookupUserResponse/></NCIPMessage>`,
		`<NCIPMessage><LookupUserResponse/></NCIPMessage><NCIPMessage/>`,
		`<NCIPMessage>unexpected<LookupUserResponse/></NCIPMessage>`,
		`<NCIPMessage><LookupUserResponse/></NCIPMessage>trailing`,
		`<NCIPMessage><LookupUserResponse></NCIPMessage>`,
		`<NCIPMessage>` + strings.Repeat("<a>", 65) + strings.Repeat("</a>", 65) + `</NCIPMessage>`,
	} {
		require.Error(t, decode([]byte(body), &ncip.NCIPMessage{}), body)
	}
	for _, version := range []string{"", ncip.NCIP_V1_0_DTD} {
		require.NoError(t, decode([]byte(`<NCIPMessage version="`+version+`"><LookupUserResponse/></NCIPMessage>`), &ncip.NCIPMessage{}))
	}
}
func TestLoggingAndDefaults(t *testing.T) {
	require.Equal(t, 30*time.Second, New(nil, "https://example.org").client.Timeout)
	custom := &http.Client{Timeout: time.Second}
	require.Same(t, custom, New(custom, "").client)
	c := testClient(t, `<NCIPMessage><LookupUserResponse><UserOptionalFields><NameInformation><PersonalNameInformation><UnstructuredPersonalUserName>secret-name</UnstructuredPersonalUserName></PersonalNameInformation></NameInformation><UserAddressInformation><ElectronicAddress><ElectronicAddressData>secret-email</ElectronicAddressData></ElectronicAddress></UserAddressInformation></UserOptionalFields></LookupUserResponse></NCIPMessage>`, nil)
	c.SetLogFunc(func(out, in map[string]any, err error) {
		require.NoError(t, err)
		data, e := json.Marshal([]any{out, in})
		require.NoError(t, e)
		for _, secret := range []string{"secret-agency", "secret-password", "secret-name", "secret-email"} {
			require.NotContains(t, string(data), secret)
		}
		require.Contains(t, string(data), "***")
	})
	arg := ncip.LookupUser{InitiationHeader: ncip.InitiationHeader{FromAgencyAuthentication: "secret-agency"}, AuthenticationInput: []ncip.AuthenticationInput{{AuthenticationInputData: "secret-password"}}}
	_, err := c.LookupUser(arg)
	require.NoError(t, err)
	require.Equal(t, "secret-password", arg.AuthenticationInput[0].AuthenticationInputData)
	c.SetLogFunc(nil)
	_, err = c.LookupUser(arg)
	require.NoError(t, err)
}
