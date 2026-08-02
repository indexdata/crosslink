package test

import (
	"net/http"
	"testing"
)

func TestEntryEnvironment(t *testing.T) {

	const symbolAuthorityEnv string = "TENANT_SYMBOL_AUTHORITY"

	shooInstitutionHeaders := map[string]string{
		"X-Okapi-Tenant":      "SHOO",
		"X-Okapi-Permissions": `["directory.institution.all"]`,
	}

	testInstitutionHeaders := map[string]string{
		"X-Okapi-Tenant":      "TEST",
		"X-Okapi-Permissions": `["directory.institution.all"]`,
	}

	t.Setenv(symbolAuthorityEnv, "GRONK")
	t.Run("CreateAndRetrieveEntry", func(t *testing.T) {
		resetDb()
		postRes, postData := jsonReq(t, http.MethodPost, "/entries",
			`{
				"name":"New Authority Institution",
				"symbols": [
					{"authority": "GRONK", "symbol": "SHOO"}
				],
				"endpoints": [
					{
						"name": "Primary",
						"type": "ISO18626",
						"address" : "https://inside.you.is/twowolves"
					}
				]
			}`,
			consortiumPermissionHeaders)

		if postRes.StatusCode != http.StatusCreated {
			t.Errorf("POST failed: %d %s", postRes.StatusCode, postData)
		}

		getRes, getData := jsonReq(t, http.MethodGet, "/entries/by-symbol/GRONK:SHOO", "", shooInstitutionHeaders)

		if getRes.StatusCode != http.StatusOK {
			t.Errorf("GET failed: %d %s", getRes.StatusCode, getData)
		}

		patchRes, patchData := jsonReq(t, http.MethodPatch, "/entries/by-symbol/GRONK:SHOO",
			`{
				"name":"New Authority Institution",
				"symbols": [
					{"authority": "GRONK", "symbol": "SHOO"}
				],
				"endpoints": [
					{
						"name": "Primary",
						"type": "ISO18626",
						"address" : "https://inside.you.is/twowolves"
					}
				]
			}`,
			shooInstitutionHeaders)

		if patchRes.StatusCode != http.StatusNoContent {
			t.Errorf("PATCH failed: %d %s", patchRes.StatusCode, patchData)
		}

		patch2Res, patch2Data := jsonReq(t, http.MethodPatch, "/entries/by-symbol/GRONK:SHOO",
			`{
				"name":"New Authority Institution",
				"symbols": [
					{"authority": "GRONK", "symbol": "SHOO"}
				],
				"endpoints": [
					{
						"name": "Primary",
						"type": "ISO18626",
						"address" : "https://inside.you.is/twowolves"
					}
				]
			}`,
			testInstitutionHeaders)

		if patch2Res.StatusCode != http.StatusUnauthorized {
			t.Errorf("PATCH2 expected 401: %d %s", patch2Res.StatusCode, patch2Data)
		}

	})
}
