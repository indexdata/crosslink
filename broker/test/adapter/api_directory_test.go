package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	test "github.com/indexdata/crosslink/broker/test/utils"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
)

var respBody []byte
var dirEntries dirapi.EntriesResponse

func TestMain(m *testing.M) {
	respBody, _ = os.ReadFile("../testdata/api-directory-response.json")
	err := json.Unmarshal(respBody, &dirEntries)
	test.Expect(err, "failed to read directory entries")
	code := m.Run()
	os.Exit(code)
}

func createDirectoryAdapter(urls ...string) adapter.DirectoryLookupAdapter {
	return adapter.CreateApiDirectory(http.DefaultClient, urls)
}

func createLookupCtx() common.ExtendedContext {
	return common.CreateExtCtxWithArgs(context.Background(), nil)
}

func boolPtr(v bool) *bool {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func withNetworkReciprocal(entry dirapi.Entry, reciprocal *bool) dirapi.Entry {
	if entry.Networks == nil {
		return entry
	}
	networks := slices.Clone(*entry.Networks)
	for i := range networks {
		networks[i].Reciprocal = reciprocal
	}
	entry.Networks = &networks
	return entry
}

func TestGetVendorFromUrl(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected dirapi.EntryVendor
	}{
		{
			name:     "alma",
			url:      "https://example.alma.exlibrisgroup.com/view/uresolver/01TEST_INST/openurl",
			expected: dirapi.Alma,
		},
		{
			name:     "rapido",
			url:      "https://example.rapido.exlibrisgroup.com/iso18626",
			expected: dirapi.Alma,
		},
		{
			name:     "reshare",
			url:      "https://tenant-okapi.example.org/_/invoke/tenant/test/rs/externalApi/iso18626",
			expected: dirapi.ReShare,
		},
		{
			name:     "illiad atlas-sys",
			url:      "https://example.Atlas-Sys.com/ILLiad/ISO18626",
			expected: dirapi.ILLiad,
		},
		{
			name:     "illiad path",
			url:      "https://example.org/ILLIAD/iso18626",
			expected: dirapi.ILLiad,
		},
		{
			name:     "unknown",
			url:      "https://example.org/iso18626",
			expected: dirapi.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, adapter.GetVendorFromUrl(tt.url))
		})
	}
}

func TestGetBrokerMode(t *testing.T) {
	assert.Equal(t, common.BrokerModeOpaque, adapter.GetBrokerMode(dirapi.ILLiad))
	assert.Equal(t, common.BrokerModeTransparent, adapter.GetBrokerMode(dirapi.CrossLink))
}

func TestLookup400(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("Invalid request"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	_, _, err := ad.Lookup(createLookupCtx(), p)
	assert.ErrorContains(t, err, "400")
}

func TestLookupInvalidUrl(t *testing.T) {
	ad := createDirectoryAdapter("invalid")
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	_, _, err := ad.Lookup(createLookupCtx(), p)
	assert.ErrorContains(t, err, "unsupported protocol scheme")
}

func TestLookupInvalidJson(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("{"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	_, _, err := ad.Lookup(createLookupCtx(), p)
	assert.ErrorContains(t, err, "unexpected end of JSON input")
}

func TestLookupEmptyList(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		resp := dirapi.EntriesResponse{}
		respBytes, _ := json.Marshal(resp)
		w.Write(respBytes)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, _, err := ad.Lookup(createLookupCtx(), p)
	assert.Nil(t, err)
	assert.Len(t, entries, 0)
}

func TestLookupNoVendor(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		respBody := "{\"items\":[{" +
			"\"name\":\"Peer\",\"symbols\":[{\"authority\":\"ISIL\",\"symbol\":\"PEER\"}]}]," +
			"\"resultInfo\":{\"totalRecords\":1}}"
		w.Write([]byte(respBody))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, _, err := ad.Lookup(createLookupCtx(), p)
	assert.Nil(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, dirapi.Unknown, entries[0].Vendor)
	assert.Equal(t, common.BrokerMode(""), entries[0].BrokerMode)
}

func TestLookupWithVendor(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		respBody := "{\"items\":[{" +
			"\"name\":\"Peer\",\"vendor\":\"ReShare\",\"symbols\":[{\"authority\":\"ISIL\",\"symbol\":\"PEER\"}]}]," +
			"\"resultInfo\":{\"totalRecords\":1}}"
		w.Write([]byte(respBody))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, _, err := ad.Lookup(createLookupCtx(), p)
	assert.Nil(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, dirapi.ReShare, entries[0].Vendor)
	assert.Equal(t, common.BrokerModeTransparent, entries[0].BrokerMode)
}

func TestLookupMissingSymbols(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		respBody := "{\"items\":[{\"endpoints\":[{\"address\":\"http://localhost:8083/\"}]," +
			"\"name\":\"Peer\"}]," +
			"\"resultInfo\":{\"totalRecords\":1}}"
		w.Write([]byte(respBody))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
		Tenant:  "tenant1",
	}
	entries, cql, err := ad.Lookup(createLookupCtx(), p)
	assert.Nil(t, err)
	assert.Len(t, entries, 0)
	assert.Equal(t, "?limit=1000&cql=symbol+any+%22ISIL%3APEER%22+and+tenant%3D%22tenant1%22", cql)
}

func TestLookupSendsSystemAndTenantHeaders(t *testing.T) {
	var permissionsHeader string
	var tenantHeader string
	var rawQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permissionsHeader = r.Header.Get("X-Okapi-Permissions")
		tenantHeader = r.Header.Get("X-Okapi-Tenant")
		rawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"about":{"count":0},"items":[]}`))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
		Tenant:  "tenant1",
	}
	_, cql, err := ad.Lookup(createLookupCtx(), p)

	assert.NoError(t, err)
	assert.Equal(t, `["directory.system.all"]`, permissionsHeader)
	assert.Equal(t, "tenant1", tenantHeader)
	assert.Equal(t, "limit=1000&cql=symbol+any+%22ISIL%3APEER%22+and+tenant%3D%22tenant1%22", rawQuery)
	assert.Equal(t, "?"+rawQuery, cql)
}

func TestLookupDeserializesDirectoryConfigurationFields(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		respBody := `{
			"about":{"count":1},
			"items":[{
				"id":"00000000-0000-0000-0000-000000000001",
				"name":"Configured Peer",
				"type":"Institution",
				"fromEmail":"from@example.org",
				"tenant":"tenant1",
				"vendor":"CrossLink",
				"symbols":[{"authority":"ISIL","symbol":"PEER"}],
				"lenderOfLastResort":[{"authority":"ISIL","symbol":"LOR"}],
				"lmsConfig":{
					"address":"https://lms.example.org",
					"fromAgency":"FROM",
					"fromAgencyAuthentication":"secret",
					"toAgency":"TO"
				},
				"holdingsConfig":{
					"metadataUpdateMode":"merge",
					"zoom":{
						"address":"z3950.example.org:210/catalog",
						"options":{
							"preferredRecordSyntax":"usmarc",
							"count":"20",
							"location":"STACKS"
						}
					},
					"queryConfig":{
						"type":"cql",
						"identifier":"rec.id = {term}",
						"isbn":"isbn = {term}",
						"issn":"issn = {term}",
						"title":"title = {term}"
					},
					"holdingsFormat":{
						"marc":{
							"mainField":"999",
							"itemIdSubField":"i",
							"locationSubField":"l",
							"callNumberSubField":"c",
							"restrictedSubField":"r",
							"shelvingLocationSubField":"s"
						},
						"opac":{}
					},
					"metadataFormat":{
						"marc21":{
							"title":"245$a",
							"author":"100$a",
							"identifier":"001",
							"isbn":"020$a",
							"issn":"022$a",
							"edition":"250$a",
							"subtitle":"245$b"
						}
					}
				}
			}]
		}`
		w.Write([]byte(respBody))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	entries, _, err := ad.Lookup(createLookupCtx(), adapter.DirectoryLookupParams{Symbols: []string{"ISIL:PEER"}})

	assert.NoError(t, err)
	if assert.Len(t, entries, 1) {
		customData := entries[0].CustomData
		assert.Equal(t, "from@example.org", *customData.FromEmail)
		assert.Equal(t, "tenant1", *customData.Tenant)
		assert.Equal(t, dirapi.CrossLink, *customData.Vendor)
		assert.Equal(t, "LOR", (*customData.LenderOfLastResort)[0].Symbol)
		assert.Equal(t, "https://lms.example.org", customData.LmsConfig.Address)
		assert.Equal(t, "secret", *customData.LmsConfig.FromAgencyAuthentication)

		holdingsConfig := customData.HoldingsConfig
		if assert.NotNil(t, holdingsConfig) {
			assert.Equal(t, dirapi.Merge, *holdingsConfig.MetadataUpdateMode)
			assert.Equal(t, "z3950.example.org:210/catalog", holdingsConfig.Zoom.Address)
			assert.Equal(t, "usmarc", (*holdingsConfig.Zoom.Options)["preferredRecordSyntax"])
			assert.Equal(t, "20", (*holdingsConfig.Zoom.Options)["count"])
			assert.Equal(t, "STACKS", (*holdingsConfig.Zoom.Options)["location"])
			assert.Equal(t, dirapi.Cql, *holdingsConfig.QueryConfig.Type)
			assert.Equal(t, "rec.id = {term}", *holdingsConfig.QueryConfig.Identifier)
			assert.Equal(t, "999", *holdingsConfig.HoldingsFormat.Marc.MainField)
			assert.NotNil(t, holdingsConfig.HoldingsFormat.Opac)
			assert.Equal(t, "245$a", *holdingsConfig.MetadataFormat.Marc21.Title)
			assert.Equal(t, "100$a", *holdingsConfig.MetadataFormat.Marc21.Author)
		}
	}
}

func TestLookupDefaultsEmptyAuthorityToISIL(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		respBody := "{\"items\":[{" +
			"\"name\":\"Peer\",\"symbols\":[{\"authority\":\"\",\"symbol\":\"PEER\"}]}]," +
			"\"resultInfo\":{\"totalRecords\":1}}"
		w.Write([]byte(respBody))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, _, err := ad.Lookup(createLookupCtx(), p)
	assert.Nil(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, []string{"ISIL:PEER"}, entries[0].Symbols)
}

func TestLookup(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(respBody)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	adapter.DEFAULT_BROKER_MODE = common.BrokerModeTransparent
	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, _, err := ad.Lookup(createLookupCtx(), p)
	assert.Nil(t, err)
	assert.Len(t, entries, 6)
	assert.Equal(t, entries[0].Name, "Albury City Libraries")
	assert.Equal(t, entries[0].Vendor, dirapi.ReShare)
	assert.Len(t, entries[0].Symbols, 1)
	assert.Equal(t, common.BrokerModeTransparent, entries[0].BrokerMode)
	assert.Equal(t, entries[3].Name, "University of Melbourne")
	assert.Len(t, entries[3].Symbols, 1)
	assert.Len(t, entries[3].BranchSymbols, 2)

	ad = createDirectoryAdapter(server.URL, server.URL)
	p = adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, _, err = ad.Lookup(createLookupCtx(), p)
	assert.Nil(t, err)
	assert.Len(t, entries, 12)
	assert.Equal(t, entries[0].Name, "Albury City Libraries")
	assert.Equal(t, entries[0].Vendor, dirapi.ReShare)
	assert.Len(t, entries[0].Symbols, 1)
}

func TestFilterAndSort(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, Symbol: "AU-NALB", CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.8, Symbol: "AU-NU", CustomData: dirEntries.Items[2]},
		{PeerId: "3", Ratio: 0.7, Symbol: "AU-VVWA", CustomData: dirEntries.Items[4]}}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Rush",
		},
		ServiceType: iso18626.TypeServiceTypeCopy,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 4480, //using this rather than 44.5 to trigger the floating point conversion issue
				Exp:  2,
			},
		},
	}
	var rotaInfo adapter.RotaInfo
	entries, rotaInfo = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 2)
	assert.Equal(t, "2", entries[0].PeerId)
	assert.Equal(t, "3", entries[1].PeerId)

	assert.Equal(t, "copy", rotaInfo.Request.Type)
	assert.Equal(t, "rush", rotaInfo.Request.Level)
	assert.Equal(t, "44.80", rotaInfo.Request.Cost)
	assert.Contains(t, rotaInfo.Requester.Networks, "NSW & ACT", "Queensland", "Victoria")
	assert.Len(t, rotaInfo.Requester.Networks, 3)
	assert.Len(t, rotaInfo.Suppliers, 3)

	sup := rotaInfo.Suppliers[2]
	assert.Equal(t, "AU-NALB", sup.Symbol)
	assert.False(t, sup.Match)
	assert.Len(t, sup.Networks, 3)
	assert.Equal(t, sup.Networks[0], adapter.NetworkMatch{Name: "NSW & ACT", Priority: 1, Match: true})
	assert.Equal(t, sup.Networks[1], adapter.NetworkMatch{Name: "Victoria", Priority: 2, Match: true})
	assert.Equal(t, sup.Networks[2], adapter.NetworkMatch{Name: "Queensland", Priority: 3, Match: true})
	assert.Equal(t, "", sup.Cost)
	assert.Equal(t, 1, sup.Priority)
	assert.Equal(t, float32(0.5), sup.Ratio)
	assert.Len(t, sup.Tiers, 4)

	sup = rotaInfo.Suppliers[1]
	assert.Equal(t, "AU-VVWA", sup.Symbol)
	assert.True(t, sup.Match)
	assert.Len(t, sup.Networks, 4)
	assert.Equal(t, sup.Networks[0], adapter.NetworkMatch{Name: "Victoria", Priority: 1, Match: true})
	assert.Equal(t, sup.Networks[1], adapter.NetworkMatch{Name: "Victoria Govt & Arts", Priority: 2, Match: false})
	assert.Equal(t, sup.Networks[2], adapter.NetworkMatch{Name: "Victoria Health", Priority: 3, Match: false})
	assert.Equal(t, sup.Networks[3], adapter.NetworkMatch{Name: "National", Priority: 9, Match: false})
	assert.Equal(t, "44.80", sup.Cost)
	assert.Equal(t, 2, sup.Priority)
	assert.Equal(t, float32(0.7), sup.Ratio)
	assert.Len(t, sup.Tiers, 4)
	assert.Equal(t, sup.Tiers[0], adapter.TierMatch{Name: "Premium Pay for Peer - Rush Copy", Level: "rush", Type: "copy", Cost: "44.80", Match: true})
	assert.Equal(t, sup.Tiers[1], adapter.TierMatch{Name: "Reciprocal Peer to Peer - Core Copy", Level: "core", Type: "copy", Cost: "0.00", Match: false})
	assert.Equal(t, sup.Tiers[2], adapter.TierMatch{Name: "Reciprocal Peer to Peer - Rush Copy", Level: "rush", Type: "copy", Cost: "0.00", Match: false})
	assert.Equal(t, sup.Tiers[3], adapter.TierMatch{Name: "Premium Pay for Peer - Core Copy", Level: "core", Type: "copy", Cost: "22.40", Match: false})

	sup = rotaInfo.Suppliers[0]
	assert.Equal(t, "AU-NU", sup.Symbol)
	assert.True(t, sup.Match)
	assert.Len(t, sup.Networks, 6)
	assert.Equal(t, sup.Networks[0], adapter.NetworkMatch{Name: "NSW & ACT", Priority: 1, Match: true})
	assert.Equal(t, sup.Networks[1], adapter.NetworkMatch{Name: "Victoria", Priority: 2, Match: true})
	assert.Equal(t, sup.Networks[2], adapter.NetworkMatch{Name: "Queensland", Priority: 3, Match: true})
	assert.Equal(t, sup.Networks[3], adapter.NetworkMatch{Name: "South Australia", Priority: 4, Match: false})
	assert.Equal(t, sup.Networks[4], adapter.NetworkMatch{Name: "Western Australia", Priority: 5, Match: false})
	assert.Equal(t, sup.Networks[5], adapter.NetworkMatch{Name: "National", Priority: 9, Match: false})
	assert.Equal(t, "44.80", sup.Cost)
	assert.Equal(t, 1, sup.Priority)
	assert.Equal(t, float32(0.8), sup.Ratio)
	assert.Len(t, sup.Tiers, 4)
	assert.Equal(t, sup.Tiers[0], adapter.TierMatch{Name: "Premium Pay for Peer - Rush Copy", Level: "rush", Type: "copy", Cost: "44.80", Match: true})
	assert.Equal(t, sup.Tiers[1], adapter.TierMatch{Name: "Premium Pay for Peer - Core Copy", Level: "core", Type: "copy", Cost: "22.40", Match: false})
	assert.Equal(t, sup.Tiers[2], adapter.TierMatch{Name: "Premium Pay for Peer - Core Loan", Level: "core", Type: "loan", Cost: "34.40", Match: false})
	assert.Equal(t, sup.Tiers[3], adapter.TierMatch{Name: "Premium Pay for Peer - Rush Loan", Level: "rush", Type: "loan", Cost: "62.80", Match: false})

	bytes, err := json.MarshalIndent(rotaInfo, "", "  ")
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "\"request\"")
}

func TestFilterAndSortFilterByCost(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]}, {PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[1]}}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Core",
		},
		ServiceType: iso18626.TypeServiceTypeLoan,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 3440,
				Exp:  2,
			},
		},
	}
	var rotaInfo adapter.RotaInfo
	entries, rotaInfo = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "loan", rotaInfo.Request.Type)
}

func TestFilterAndSortFilterByCost0(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]}, {PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[1]}}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Core",
		},
		ServiceType: iso18626.TypeServiceTypeLoan,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 000,
				Exp:  2,
			},
		},
	}
	var rotaInfo adapter.RotaInfo
	entries, rotaInfo = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "loan", rotaInfo.Request.Type)
}

func TestFilterAndSortByType(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[2]},
		{PeerId: "3", Ratio: 0.7, CustomData: dirEntries.Items[4]}}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Core",
		},
		ServiceType: iso18626.TypeServiceTypeLoan,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 3500,
				Exp:  2,
			},
		},
	}
	var rotaInfo adapter.RotaInfo
	entries, rotaInfo = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 2)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "2", entries[1].PeerId)
	assert.Equal(t, "loan", rotaInfo.Request.Type)
}

func TestFilterAndSortByLevel(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[2]},
		{PeerId: "3", Ratio: 0.7, CustomData: dirEntries.Items[4]}}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Rush",
		},
		ServiceType: iso18626.TypeServiceTypeCopy,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 4500,
				Exp:  2,
			},
		},
	}
	var rotaInfo adapter.RotaInfo
	entries, rotaInfo = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 2)
	assert.Equal(t, "2", entries[0].PeerId)
	assert.Equal(t, "3", entries[1].PeerId)
	assert.Equal(t, "copy", rotaInfo.Request.Type)
}

func TestFilterAndSortReciprocal(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[4]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[2]},
		{PeerId: "3", Ratio: 0.7, CustomData: dirEntries.Items[4]}}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Rush",
		},
		ServiceType: iso18626.TypeServiceTypeCopy,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 0,
				Exp:  2,
			},
		},
	}
	var rotaInfo adapter.RotaInfo
	entries, rotaInfo = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "3", entries[0].PeerId)
	assert.Equal(t, "copy", rotaInfo.Request.Type)
}

func TestFilterAndSortReciprocalNetworkExcludesPaidTiers(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := withNetworkReciprocal(dirEntries.Items[0], boolPtr(true))
	entries := []adapter.Supplier{
		{PeerId: "2", Ratio: 0.7, Symbol: "AU-NU", CustomData: dirEntries.Items[2]},
	}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Core",
		},
		ServiceType: iso18626.TypeServiceTypeLoan,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 3500,
				Exp:  2,
			},
		},
	}

	entries, rotaInfo := ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)

	assert.Len(t, entries, 1)
	assert.Len(t, rotaInfo.Suppliers, 1)
	assert.True(t, rotaInfo.Suppliers[0].Match)
}

func TestFilterAndSortPaidNetworkExcludesFreeTiers(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := withNetworkReciprocal(dirEntries.Items[4], boolPtr(false))
	entries := []adapter.Supplier{
		{PeerId: "3", Ratio: 0.7, Symbol: "AU-VVWA", CustomData: dirEntries.Items[4]},
	}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Rush",
		},
		ServiceType: iso18626.TypeServiceTypeCopy,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 0,
				Exp:  2,
			},
		},
	}

	entries, rotaInfo := ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)

	assert.Len(t, entries, 1)
	assert.Len(t, rotaInfo.Suppliers, 1)
	assert.True(t, rotaInfo.Suppliers[0].Match)
}

func TestFilterAndSortPaidNetworkAllowsPaidTiers(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := withNetworkReciprocal(dirEntries.Items[0], boolPtr(false))
	entries := []adapter.Supplier{
		{PeerId: "2", Ratio: 0.7, Symbol: "AU-NU", CustomData: withNetworkReciprocal(dirEntries.Items[2], boolPtr(false))},
	}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "standard",
		},
		ServiceType: iso18626.TypeServiceTypeLoan,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 3500,
				Exp:  2,
			},
		},
	}

	entries, rotaInfo := ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)

	assert.Empty(t, entries)
	assert.Len(t, rotaInfo.Suppliers, 1)
	assert.False(t, rotaInfo.Suppliers[0].Match)
}

func TestFilterAndSortUsesCompatibleNetworkPriority(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterNetworks := []dirapi.Network{
		{Name: stringPtr("Reciprocal"), Priority: 1, Reciprocal: boolPtr(true)},
		{Name: stringPtr("Paid Low"), Priority: 5, Reciprocal: boolPtr(false)},
		{Name: stringPtr("Paid High"), Priority: 3, Reciprocal: boolPtr(false)},
	}
	paidTier := []dirapi.Tier{
		{Name: stringPtr("Paid Core Loan"), Level: dirapi.Standard, Type: dirapi.Loan, Cost: 34.4},
	}
	requesterData := dirapi.Entry{Name: "Requester", Networks: &requesterNetworks}
	supplierANetworks := []dirapi.Network{
		{Name: stringPtr("Reciprocal"), Priority: 1, Reciprocal: boolPtr(true)},
		{Name: stringPtr("Paid Low"), Priority: 5, Reciprocal: boolPtr(false)},
	}
	supplierBNetworks := []dirapi.Network{
		{Name: stringPtr("Paid High"), Priority: 3, Reciprocal: boolPtr(false)},
	}
	entries := []adapter.Supplier{
		{PeerId: "A", Symbol: "A", CustomData: dirapi.Entry{Name: "Supplier A", Networks: &supplierANetworks, Tiers: &paidTier}},
		{PeerId: "B", Symbol: "B", CustomData: dirapi.Entry{Name: "Supplier B", Networks: &supplierBNetworks, Tiers: &paidTier}},
	}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Core",
		},
		ServiceType: iso18626.TypeServiceTypeLoan,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 3500,
				Exp:  2,
			},
		},
	}

	entries, _ = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)

	assert.Empty(t, entries)
}

func TestFilterAndSortNoFilters(t *testing.T) {
	appCtx := createLookupCtx()
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[2]},
		{PeerId: "3", Ratio: 0.8, CustomData: dirEntries.Items[4]}}
	var rotaInfo adapter.RotaInfo
	entries, rotaInfo = ad.FilterAndSort(appCtx, entries, requesterData, nil, nil)
	assert.Len(t, entries, 2)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "3", entries[1].PeerId)
	assert.Equal(t, "", rotaInfo.Request.Type)
}

func TestCompareSuppliers(t *testing.T) {
	assert.True(t, adapter.CompareSuppliers(adapter.Supplier{}, adapter.Supplier{}) == 0)
	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1}) == 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 0, Priority: 1, Ratio: 1},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 0, Priority: 1, Ratio: 1},
		adapter.Supplier{Cost: 1, Priority: 0, Ratio: 0}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, Priority: 0, Ratio: 1},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, Priority: 0, Ratio: 1},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 0}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 0},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 2, Priority: 1, Ratio: 1},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1}) > 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, Priority: 2, Ratio: 1},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1}) > 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 2},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1}) > 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1, Local: true},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1, Local: false}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1, Local: false},
		adapter.Supplier{Cost: 1, Priority: 1, Ratio: 1, Local: true}) > 0)

	suppliers := []adapter.Supplier{{Cost: 1, Priority: 1, Ratio: 1, Local: false}, {Cost: 1, Priority: 1, Ratio: 1, Local: true}}
	slices.SortFunc(suppliers, func(a, b adapter.Supplier) int {
		return adapter.CompareSuppliers(a, b)
	})
	assert.True(t, suppliers[0].Local) // Local sorted as first
	assert.False(t, suppliers[1].Local)
}
