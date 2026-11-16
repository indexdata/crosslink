package ncipclient

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/require"
)

type loggingTransport func(*http.Request) (*http.Response, error)

func (f loggingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func loggingClient(body string, status int, namespaceFree bool) NcipClient {
	return NewNcipClient(&http.Client{Transport: loggingTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, "https://lms.example/ncip", "broker", "lms", "secret-agency", namespaceFree)
}

func TestLoggingShippingAddresses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		secret   string
		shipping ncip.ShippingInformation
	}{
		{"PhysicalAddress", "123 Private Street", ncip.ShippingInformation{PhysicalAddress: &ncip.PhysicalAddress{
			UnstructuredAddress: &ncip.UnstructuredAddress{UnstructuredAddressData: "123 Private Street"},
		}}},
		{"ElectronicAddress", "private@example.org", ncip.ShippingInformation{ElectronicAddress: &ncip.ElectronicAddress{ElectronicAddressData: "private@example.org"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := ncip.NCIPMessage{RequestItemResponse: &ncip.RequestItemResponse{ShippingInformation: &tc.shipping}}
			body, err := xml.Marshal(message)
			require.NoError(t, err)
			client := loggingClient(string(body), http.StatusOK, false)
			logged := 0
			client.SetLogFunc(func(out, in map[string]any, err error) {
				logged++
				require.NoError(t, err)
				data, err := json.Marshal([]any{out, in})
				require.NoError(t, err)
				require.NotContains(t, string(data), tc.secret)
				require.NotContains(t, string(data), "secret-agency")
				shipping := in["RequestItemResponse"].(map[string]any)["ShippingInformation"].(map[string]any)
				require.Equal(t, "***", shipping[tc.name])
			})
			response, err := client.RequestItem(ncip.RequestItem{})
			require.NoError(t, err)
			require.Equal(t, 1, logged)
			data, err := json.Marshal(response.ShippingInformation)
			require.NoError(t, err)
			require.Contains(t, string(data), tc.secret)
		})
	}
}

func TestLoggingDateOfBirthAndAuthentication(t *testing.T) {
	// Use typed decoding so this also exercises the date's JSON representation.
	body := `<NCIPMessage xmlns="http://www.niso.org/2008/ncip"><LookupUserResponse><UserOptionalFields><DateOfBirth>1980-01-02T00:00:00Z</DateOfBirth></UserOptionalFields></LookupUserResponse></NCIPMessage>`
	client := loggingClient(body, http.StatusOK, false)
	logged := 0
	client.SetLogFunc(func(out, in map[string]any, err error) {
		logged++
		require.NoError(t, err)
		data, err := json.Marshal([]any{out, in})
		require.NoError(t, err)
		require.NotContains(t, string(data), "1980")
		require.NotContains(t, string(data), "secret-")
		fields := in["LookupUserResponse"].(map[string]any)["UserOptionalFields"].(map[string]any)
		require.Equal(t, "***", fields["DateOfBirth"])
	})
	arg := ncip.LookupUser{AuthenticationInput: []ncip.AuthenticationInput{
		{AuthenticationInputType: ncip.SchemeValuePair{Text: "username"}, AuthenticationInputData: "secret-user"},
		{AuthenticationInputData: "secret-untyped"},
	}}
	response, err := client.LookupUser(arg)
	require.NoError(t, err)
	require.Equal(t, 1, logged)
	require.NotNil(t, response.UserOptionalFields.DateOfBirth)
	require.Equal(t, "secret-user", arg.AuthenticationInput[0].AuthenticationInputData)
}

func TestLoggingProblemData(t *testing.T) {
	for _, topLevel := range []bool{false, true} {
		t.Run(fmt.Sprint(topLevel), func(t *testing.T) {
			problem := ncip.Problem{ProblemType: ncip.SchemeValuePair{Text: "Rejected"}, ProblemDetail: "secret-detail", ProblemValue: "secret-value"}
			message := ncip.NCIPMessage{LookupUserResponse: &ncip.LookupUserResponse{Problem: []ncip.Problem{problem}}}
			if topLevel {
				message = ncip.NCIPMessage{Problem: []ncip.Problem{problem}}
			}
			body, err := xml.Marshal(message)
			require.NoError(t, err)
			client := loggingClient(string(body), http.StatusOK, false)
			var logged error
			client.SetLogFunc(func(_, in map[string]any, err error) {
				logged = err
				data, e := json.Marshal(in)
				require.NoError(t, e)
				require.NotContains(t, string(data), "secret-")
				require.Contains(t, string(data), `"ProblemDetail":"***"`)
				require.Contains(t, string(data), `"ProblemValue":"***"`)
			})
			response, returned := client.LookupUser(ncip.LookupUser{})
			for _, err := range []error{returned, logged} {
				require.ErrorContains(t, err, "Rejected")
				require.NotContains(t, err.Error(), "secret-")
				var cause *NcipError
				require.ErrorAs(t, err, &cause)
				require.Equal(t, "secret-detail", cause.Problem.ProblemDetail)
				require.Equal(t, "secret-value", cause.Problem.ProblemValue)
			}
			if !topLevel {
				require.Equal(t, "secret-detail", response.Problem[0].ProblemDetail)
			}
		})
	}
}

func TestLoggingResponseFailures(t *testing.T) {
	for _, namespaceFree := range []bool{false, true} {
		for _, tc := range []struct {
			name, body string
			status     int
		}{
			{"http", "<html>secret-echo</html>", http.StatusUnauthorized},
			{"syntax", `<NCIPMessage xmlns="http://www.niso.org/2008/ncip"><secret-echo></wrong></NCIPMessage>`, http.StatusOK},
			{"partial", `<NCIPMessage xmlns="http://www.niso.org/2008/ncip"><LookupUserResponse><UserId><UserIdentifierValue>secret-echo</UserIdentifierValue></UserId><UserOptionalFields><DateOfBirth>&secret-date;</DateOfBirth></UserOptionalFields></LookupUserResponse></NCIPMessage>`, http.StatusOK},
		} {
			t.Run(fmt.Sprintf("%t/%s", namespaceFree, tc.name), func(t *testing.T) {
				body := tc.body
				if namespaceFree {
					body = strings.ReplaceAll(body, ` xmlns="http://www.niso.org/2008/ncip"`, "")
				}
				client := loggingClient(body, tc.status, namespaceFree)
				var logged error
				client.SetLogFunc(func(_, in map[string]any, err error) {
					logged = err
					data, e := json.Marshal(in)
					require.NoError(t, e)
					require.NotContains(t, string(data), "secret-")
				})
				_, returned := client.LookupUser(ncip.LookupUser{})
				for _, err := range []error{returned, logged} {
					require.Error(t, err)
					for cause := err; cause != nil; cause = errors.Unwrap(cause) {
						require.NotContains(t, fmt.Sprintf("%+v", cause), "secret-")
					}
					if tc.status != http.StatusOK {
						var cause *httpclient.HttpError
						require.ErrorAs(t, err, &cause)
						require.Equal(t, tc.status, cause.StatusCode)
						require.Empty(t, cause.Body)
					} else {
						require.ErrorContains(t, err, "invalid NCIP v2 response")
					}
				}
			})
		}
	}
}

func TestLoggingTransportError(t *testing.T) {
	sentinel := errors.New("transport failed")
	client := NewNcipClient(&http.Client{Transport: loggingTransport(func(*http.Request) (*http.Response, error) {
		return nil, sentinel
	})}, "https://lms.example/ncip", "broker", "lms", "", false)
	var logged error
	client.SetLogFunc(func(_, _ map[string]any, err error) { logged = err })
	_, err := client.LookupUser(ncip.LookupUser{})
	require.ErrorIs(t, err, sentinel)
	require.ErrorIs(t, logged, sentinel)
}
