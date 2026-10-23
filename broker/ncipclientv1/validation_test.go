package ncipclientv1

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	ncip "github.com/indexdata/crosslink/ncipv1"
	"github.com/stretchr/testify/require"
)

func invokeService(c *Client, name string) error {
	switch name {
	case "LookupUser":
		_, err := c.LookupUser(ncip.LookupUser{})
		return err
	case "AcceptItem":
		_, err := c.AcceptItem(ncip.AcceptItem{})
		return err
	case "RequestItem":
		_, err := c.RequestItem(ncip.RequestItem{})
		return err
	case "CancelRequestItem":
		_, err := c.CancelRequestItem(ncip.CancelRequestItem{})
		return err
	case "CheckInItem":
		_, err := c.CheckInItem(ncip.CheckInItem{})
		return err
	case "CheckOutItem":
		_, err := c.CheckOutItem(ncip.CheckOutItem{})
		return err
	default:
		return fmt.Errorf("unknown test service %s", name)
	}
}

func TestIncompleteAcknowledgements(t *testing.T) {
	for _, tc := range []struct {
		service  string
		required []string
	}{
		{"LookupUser", []string{"UniqueUserId"}},
		{"AcceptItem", []string{"UniqueRequestId"}},
		{"RequestItem", []string{"UniqueRequestId", "UniqueUserId", "RequestType", "RequestScopeType"}},
		{"CancelRequestItem", []string{"UniqueRequestId", "UniqueUserId"}},
		{"CheckInItem", []string{"UniqueItemId"}},
		{"CheckOutItem", []string{"UniqueItemId", "UniqueUserId", "IndeterminateLoanPeriodFlag"}},
	} {
		t.Run(tc.service, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tc.service + "Response.xml")
			require.NoError(t, err)
			body := string(data)
			required := append([]string{"ResponseHeader", "FromAgencyId", "ToAgencyId", "Scheme", "Value"}, tc.required...)
			for _, field := range required {
				t.Run("missing "+field, func(t *testing.T) {
					broken := regexp.MustCompile(`(?s)<`+field+`(?:\s*/>|>.*?</`+field+`>)`).ReplaceAllString(body, "")
					require.NotEqual(t, body, broken)
					require.Error(t, invokeService(testClient(t, broken, nil), tc.service))
				})
			}
			for _, field := range []string{"UserIdentifierValue", "ItemIdentifierValue", "RequestIdentifierValue", "Scheme", "Value"} {
				if !strings.Contains(body, "<"+field+">") {
					continue
				}
				t.Run("empty "+field, func(t *testing.T) {
					broken := regexp.MustCompile(`(?s)<`+field+`>.*?</`+field+`>`).ReplaceAllString(body, "<"+field+"> </"+field+">")
					require.Error(t, invokeService(testClient(t, broken, nil), tc.service))
				})
			}
			empty := "<NCIPMessage><" + tc.service + "Response/></NCIPMessage>"
			require.Error(t, invokeService(testClient(t, empty, nil), tc.service))
			mixed := strings.Replace(body, "</"+tc.service+"Response>", "<Problem/>"+"</"+tc.service+"Response>", 1)
			require.ErrorContains(t, decode([]byte(mixed), &ncip.NCIPMessage{}), "Problem mixed")
			require.ErrorContains(t, invokeService(testClient(t, mixed, nil), tc.service), "invalid NCIP v1 response")
			repeated := strings.Replace(body, "</"+tc.service+"Response>", "<ResponseHeader/>"+"</"+tc.service+"Response>", 1)
			require.ErrorContains(t, decode([]byte(repeated), &ncip.NCIPMessage{}), "duplicate ResponseHeader")
			require.ErrorContains(t, invokeService(testClient(t, repeated, nil), tc.service), "invalid NCIP v1 response")
		})
	}
}

func TestResponseChoices(t *testing.T) {
	for _, service := range []string{"RequestItem", "CancelRequestItem"} {
		data, err := os.ReadFile("testdata/" + service + "Response.xml")
		require.NoError(t, err)
		// Both services permit a unique item ID instead of a request ID.
		body := strings.ReplaceAll(string(data), "UniqueRequestId", "UniqueItemId")
		body = strings.ReplaceAll(body, "RequestIdentifierValue", "ItemIdentifierValue")
		require.NoError(t, invokeService(testClient(t, body, nil), service))
	}
	data, err := os.ReadFile("testdata/CheckOutItemResponse.xml")
	require.NoError(t, err)
	for _, period := range []string{"<DateDue>2026-12-01T00:00:00Z</DateDue>", "<NonReturnableFlag/>"} {
		body := strings.Replace(string(data), "<IndeterminateLoanPeriodFlag />", period, 1)
		require.NoError(t, invokeService(testClient(t, body, nil), "CheckOutItem"))
	}
	mixed := strings.Replace(string(data), "<IndeterminateLoanPeriodFlag />", "<IndeterminateLoanPeriodFlag/><NonReturnableFlag/>", 1)
	require.ErrorContains(t, decode([]byte(mixed), &ncip.NCIPMessage{}), "one loan period")
	require.ErrorContains(t, invokeService(testClient(t, mixed, nil), "CheckOutItem"), "invalid NCIP v1 response")
}

func TestMalformedProblems(t *testing.T) {
	valid := `<ProcessingError><ProcessingErrorType><Scheme>local</Scheme><Value>Rejected</Value></ProcessingErrorType><ProcessingErrorElement><ElementName>UniqueUserId</ElementName></ProcessingErrorElement></ProcessingError>`
	for _, problem := range []string{
		"", "<ProcessingError/>", "<MessagingError/>",
		strings.Replace(valid, "<ElementName>UniqueUserId</ElementName>", "", 1),
		strings.Replace(valid, "<Scheme>local</Scheme>", "", 1),
		strings.ReplaceAll(valid, "Processing", "Messaging") + valid,
		strings.Replace(valid, "</ProcessingErrorElement>", "<ProcessingErrorValue>value</ProcessingErrorValue><ProcessingErrorElement><ElementName>nested</ElementName></ProcessingErrorElement></ProcessingErrorElement>", 1),
	} {
		body := `<NCIPMessage><LookupUserResponse>` + testResponseHeader + `<Problem>` + problem + `</Problem></LookupUserResponse></NCIPMessage>`
		require.ErrorContains(t, invokeService(testClient(t, body, nil), "LookupUser"), "invalid NCIP v1 response")
	}
}

func TestRedactShippingAddresses(t *testing.T) {
	fixture, err := os.ReadFile("testdata/RequestItemResponse.xml")
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		secret   string
		shipping ncip.ShippingInformation
	}{
		{
			name: "PhysicalAddress", secret: "123 Private Street",
			shipping: ncip.ShippingInformation{PhysicalAddress: &ncip.PhysicalAddress{
				UnstructuredAddress: &ncip.UnstructuredAddress{
					UnstructuredAddressType: ncip.UnstructuredAddressType{Scheme: "local", Value: "Postal"},
					UnstructuredAddressData: "123 Private Street",
				},
				PhysicalAddressType: ncip.PhysicalAddressType{Scheme: "local", Value: "Home"},
			}},
		},
		{
			name: "ElectronicAddress", secret: "private@example.org",
			shipping: ncip.ShippingInformation{ElectronicAddress: &ncip.ElectronicAddress{
				ElectronicAddressType: ncip.ElectronicAddressType{Scheme: "local", Value: "Email"},
				ElectronicAddressData: "private@example.org",
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var message ncip.NCIPMessage
			require.NoError(t, xml.Unmarshal(fixture, &message))
			message.RequestItemResponse.ShippingInformation = &tc.shipping
			body, err := xml.Marshal(message)
			require.NoError(t, err)
			c := testClient(t, string(body), nil)
			logged := 0
			c.SetLogFunc(func(out, in map[string]any, err error) {
				logged++
				require.NoError(t, err)
				data, err := json.Marshal([]any{out, in})
				require.NoError(t, err)
				require.NotContains(t, string(data), tc.secret)
				response := in["RequestItemResponse"].(map[string]any)
				shipping := response["ShippingInformation"].(map[string]any)
				require.Equal(t, "***", shipping[tc.name])
			})
			response, err := c.RequestItem(ncip.RequestItem{})
			require.NoError(t, err)
			require.Equal(t, 1, logged)
			// Redaction must not change the response available to the caller.
			data, err := json.Marshal(response.ShippingInformation)
			require.NoError(t, err)
			require.Contains(t, string(data), tc.secret)
		})
	}
}

func TestRedactEchoedErrorValues(t *testing.T) {
	for _, kind := range []string{"Messaging", "Processing"} {
		t.Run(kind, func(t *testing.T) {
			problem := fmt.Sprintf(`<Problem><%[1]sError><%[1]sErrorType><Scheme>local</Scheme><Value>Rejected</Value></%[1]sErrorType><%[1]sErrorElement><ElementName>FromAgencyAuthentication</ElementName><%[1]sErrorValue>secret-agency</%[1]sErrorValue></%[1]sErrorElement></%[1]sError><%[1]sError><%[1]sErrorType><Scheme>local</Scheme><Value>Rejected</Value></%[1]sErrorType><%[1]sErrorElement><ElementName>AuthenticationInput</ElementName><%[1]sErrorElement><ElementName>AuthenticationInputData</ElementName><%[1]sErrorValue>secret-password</%[1]sErrorValue></%[1]sErrorElement></%[1]sErrorElement></%[1]sError></Problem>`, kind)
			c := testClient(t, `<NCIPMessage><LookupUserResponse>`+testResponseHeader+problem+`</LookupUserResponse></NCIPMessage>`, nil)
			logged := 0
			c.SetLogFunc(func(out, in map[string]any, err error) {
				logged++
				require.Error(t, err)
				data, e := json.Marshal([]any{out, in})
				require.NoError(t, e)
				require.NotContains(t, string(data), "secret-agency")
				require.NotContains(t, string(data), "secret-password")
				require.Contains(t, string(data), "***")
				require.NotContains(t, err.Error(), "secret-")
			})
			response, err := c.LookupUser(ncip.LookupUser{})
			var problemError *Error
			require.ErrorAs(t, err, &problemError)
			require.Equal(t, 1, logged)
			// Logging redacts its map, not the native response returned to callers.
			data, err := json.Marshal(response.Problem)
			require.NoError(t, err)
			require.Contains(t, string(data), "secret-password")
		})
	}
}

// replaceAcknowledgementElement selects an exact path, so tests can distinguish
// the same element name in the header, returned identities, and optional metadata.
func replaceAcknowledgementElement(t *testing.T, body string, path []string, replace func(string) string) string {
	t.Helper()
	re := regexp.MustCompile("(?s)<" + path[0] + ">.*?</" + path[0] + ">")
	match := re.FindStringIndex(body)
	require.NotNil(t, match, "missing test path %v", path)
	element := body[match[0]:match[1]]
	if len(path) == 1 {
		element = replace(element)
	} else {
		element = replaceAcknowledgementElement(t, element, path[1:], replace)
	}
	return body[:match[0]] + element + body[match[1]:]
}

func TestDuplicateNestedAcknowledgementFields(t *testing.T) {
	for _, service := range []string{"LookupUser", "AcceptItem", "RequestItem", "CancelRequestItem", "CheckInItem", "CheckOutItem"} {
		t.Run(service, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + service + "Response.xml")
			require.NoError(t, err)
			body := string(data)
			paths := []string{
				"ResponseHeader/FromAgencyId", "ResponseHeader/ToAgencyId",
				"ResponseHeader/FromAgencyId/UniqueAgencyId", "ResponseHeader/ToAgencyId/UniqueAgencyId",
				"ResponseHeader/FromAgencyId/UniqueAgencyId/Scheme", "ResponseHeader/ToAgencyId/UniqueAgencyId/Value",
			}
			for _, identity := range []struct{ name, value string }{
				{"UniqueUserId", "UserIdentifierValue"},
				{"UniqueItemId", "ItemIdentifierValue"},
				{"UniqueRequestId", "RequestIdentifierValue"},
			} {
				if strings.Contains(body, "<"+identity.name+">") {
					paths = append(paths, identity.name+"/UniqueAgencyId", identity.name+"/UniqueAgencyId/Scheme",
						identity.name+"/UniqueAgencyId/Value", identity.name+"/"+identity.value)
				}
			}
			for _, path := range paths {
				for _, split := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/split=%t", path, split), func(t *testing.T) {
						broken := replaceAcknowledgementElement(t, body, strings.Split(path, "/"), func(element string) string {
							if split && strings.Contains(element, "<Scheme>") && strings.Contains(element, "<Value>") {
								// Neither repeated container holds a complete agency, but
								// encoding/xml would merge the Scheme and Value fields.
								withoutScheme := regexp.MustCompile("(?s)<Scheme>.*?</Scheme>").ReplaceAllString(element, "")
								withoutValue := regexp.MustCompile("(?s)<Value>.*?</Value>").ReplaceAllString(element, "")
								return withoutValue + withoutScheme
							}
							// Conflicting singleton text must not be silently overwritten.
							conflict := regexp.MustCompile(">[^<]+<").ReplaceAllString(element, ">conflicting<")
							return element + conflict
						})
						var decoded ncip.NCIPMessage
						require.ErrorContains(t, decode([]byte(broken), &decoded), "duplicate "+path)
						require.Equal(t, ncip.NCIPMessage{}, decoded, "reject before unmarshalling")
						client := testClient(t, broken, nil)
						var logged error
						client.SetLogFunc(func(_, _ map[string]any, err error) { logged = err })
						require.ErrorContains(t, invokeService(client, service), "invalid NCIP v1 response")
						require.ErrorContains(t, logged, "invalid NCIP v1 response")
					})
				}
			}
		})
	}
}

func TestAcknowledgementOccurrenceChecksLeaveMetadataLenient(t *testing.T) {
	body := "<NCIPMessage><LookupUserResponse>" + testResponseHeader + testResponseUser +
		"<UserOptionalFields><UserPrivilege><UniqueAgencyId><Scheme>local</Scheme><Value>one</Value></UniqueAgencyId></UserPrivilege>" +
		"<UserPrivilege><UniqueAgencyId><Scheme>local</Scheme><Value>two</Value></UniqueAgencyId></UserPrivilege></UserOptionalFields>" +
		"</LookupUserResponse></NCIPMessage>"
	response, err := testClient(t, body, nil).LookupUser(ncip.LookupUser{})
	require.NoError(t, err)
	require.Len(t, response.UserOptionalFields.UserPrivilege, 2)
	require.Equal(t, "one", response.UserOptionalFields.UserPrivilege[0].UniqueAgencyId.Value)
	require.Equal(t, "two", response.UserOptionalFields.UserPrivilege[1].UniqueAgencyId.Value)
}
