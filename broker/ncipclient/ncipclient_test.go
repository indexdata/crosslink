package ncipclient

import (
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"

	mockapp "github.com/indexdata/crosslink/illmock/app"
	"github.com/indexdata/crosslink/ncip"

	test "github.com/indexdata/crosslink/broker/test/utils"
)

func TestMain(m *testing.M) {
	mockPort := utils.Must(test.GetFreePort())

	test.Expect(os.Setenv("HTTP_PORT", strconv.Itoa(mockPort)), "failed to set mock server port")

	go func() {
		var mockApp mockapp.MockApp
		test.Expect(mockApp.Run(), "failed to start illmock server")
	}()
	test.WaitForServiceUp(mockPort)

	m.Run()
}

func TestAuthenticateUserAutoOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["authuser_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	b, err := ncipClient.AuthenticateUser(customData, lookup)
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestAuthenticateUserAutoImvalidUser(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["authuser_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "foo",
		},
	}
	_, err := ncipClient.AuthenticateUser(customData, lookup)
	assert.Error(t, err, "NCIP user authentication failed: Unknown User: foo")
}

func TestAuthenticateUserAutoManual(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["authuser_mode"] = "manual"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	b, err := ncipClient.AuthenticateUser(customData, lookup)
	assert.NoError(t, err)
	assert.False(t, b)
}

func TestAuthenticateUserDisabled(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["authuser_mode"] = "disabled"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	b, err := ncipClient.AuthenticateUser(customData, lookup)
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestAuthenticateUserMissingAddress(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["authuser_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.AuthenticateUser(customData, lookup)
	assert.Error(t, err, "missing NCIP address in customData")
}
