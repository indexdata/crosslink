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
	extctx "github.com/indexdata/crosslink/broker/common"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
)

var respBody []byte
var dirEntries adapter.EntriesResponse

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
	_, err, _ := ad.Lookup(p)
	assert.ErrorContains(t, err, "400")
}

func TestLookupInvalidUrl(t *testing.T) {
	ad := createDirectoryAdapter("invalid")
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	_, err, _ := ad.Lookup(p)
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
	_, err, _ := ad.Lookup(p)
	assert.ErrorContains(t, err, "unexpected end of JSON input")
}

func TestLookupEmptyList(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		resp := adapter.EntriesResponse{}
		respBytes, _ := json.Marshal(resp)
		w.Write(respBytes)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err, _ := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 0)
}

func TestLookupMissingUrl(t *testing.T) {
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
	entries, err, _ := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 0)
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
	}
	entries, err, cql := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 0)
	assert.Equal(t, "?maximumRecords=1000&cql=symbol+any+ISIL%3APEER", cql)
}

func TestLookup(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(respBody)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	adapter.DEFAULT_BROKER_MODE = extctx.BrokerModeTransparent
	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err, _ := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 6)
	assert.Equal(t, entries[0].Name, "Albury City Libraries")
	assert.Len(t, entries[0].Symbols, 1)
	assert.Equal(t, extctx.BrokerModeTransparent, entries[0].BrokerMode)
	assert.Equal(t, entries[3].Name, "University of Melbourne")
	assert.Len(t, entries[3].Symbols, 1)
	assert.Len(t, entries[3].BranchSymbols, 2)

	ad = createDirectoryAdapter(server.URL, server.URL)
	p = adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err, _ = ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 12)
	assert.Equal(t, entries[0].Name, "Albury City Libraries")
	assert.Len(t, entries[0].Symbols, 1)
}

func TestFilterAndSort(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
		ServiceType: iso18626.TypeServiceTypeCopy,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 3500,
				Exp:  2,
			},
		},
	}
	var matchResult adapter.MatchResult
	entries, matchResult = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 3)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "3", entries[1].PeerId)
	assert.Equal(t, "2", entries[2].PeerId)
	assert.Equal(t, "copy", matchResult.Request.ServiceType)
	assert.Equal(t, "core", matchResult.Request.ServiceLevel)
	assert.Equal(t, "35.00", matchResult.Request.Cost)
	assert.Contains(t, matchResult.Requester.Networks, "NSW & ACT", "Queensland", "Victoria")
	assert.Len(t, matchResult.Requester.Networks, 3)
	assert.Len(t, matchResult.Suppliers, 3)

	sup := matchResult.Suppliers[0]
	assert.Equal(t, "", sup.Symbol)
	assert.Len(t, sup.Networks, 3)
	assert.Equal(t, sup.Networks[0], adapter.MatchValue{Value: "NSW & ACT", Match: true})
	assert.Equal(t, sup.Networks[1], adapter.MatchValue{Value: "Queensland", Match: true})
	assert.Equal(t, sup.Networks[2], adapter.MatchValue{Value: "Victoria", Match: true})
	assert.Len(t, sup.Tiers, 4)

	sup = matchResult.Suppliers[1]
	assert.Equal(t, "", sup.Symbol)
	assert.Len(t, sup.Networks, 6)
	assert.Equal(t, sup.Networks[0], adapter.MatchValue{Value: "NSW & ACT", Match: true})
	assert.Equal(t, sup.Networks[1], adapter.MatchValue{Value: "Queensland", Match: true})
	assert.Equal(t, sup.Networks[2], adapter.MatchValue{Value: "Victoria", Match: true})
	assert.Equal(t, sup.Networks[3], adapter.MatchValue{Value: "National", Match: false})
	assert.Equal(t, sup.Networks[4], adapter.MatchValue{Value: "South Australia", Match: false})
	assert.Equal(t, sup.Networks[5], adapter.MatchValue{Value: "Western Australia", Match: false})
	assert.Len(t, sup.Tiers, 4)
	assert.Equal(t, sup.Tiers[0], adapter.MatchTier{Tier: "Premium Pay for Peer - Core Copy", ServiceLevel: "core", ServiceType: "copy", Cost: "22.40", Match: true})
	assert.Equal(t, sup.Tiers[1], adapter.MatchTier{Tier: "Premium Pay for Peer - Core Loan", ServiceLevel: "core", ServiceType: "loan", Cost: "34.40", Match: false})
	assert.Equal(t, sup.Tiers[2], adapter.MatchTier{Tier: "Premium Pay for Peer - Rush Copy", ServiceLevel: "rush", ServiceType: "copy", Cost: "44.50", Match: false})
	assert.Equal(t, sup.Tiers[3], adapter.MatchTier{Tier: "Premium Pay for Peer - Rush Loan", ServiceLevel: "rush", ServiceType: "loan", Cost: "62.80", Match: false})

	sup = matchResult.Suppliers[2]
	assert.Equal(t, "", sup.Symbol)
	assert.Len(t, sup.Networks, 4)
	assert.Equal(t, sup.Networks[0], adapter.MatchValue{Value: "Victoria", Match: true})
	assert.Equal(t, sup.Networks[1], adapter.MatchValue{Value: "National", Match: false})
	assert.Equal(t, sup.Networks[2], adapter.MatchValue{Value: "Victoria Govt & Arts", Match: false})
	assert.Equal(t, sup.Networks[3], adapter.MatchValue{Value: "Victoria Health", Match: false})
	assert.Len(t, sup.Tiers, 4)

	bytes, err := json.MarshalIndent(matchResult, "", "  ")
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "\"request\"")
}

func TestFilterAndSortFilterByCost(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
				Base: 1000,
				Exp:  2,
			},
		},
	}
	var matchMatchResult adapter.MatchResult
	entries, matchMatchResult = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "loan", matchMatchResult.Request.ServiceType)
}

func TestFilterAndSortFilterByCost0(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
	var matchMatchResult adapter.MatchResult
	entries, matchMatchResult = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "loan", matchMatchResult.Request.ServiceType)
}

func TestFilterAndSortByType(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
	var matchMatchResult adapter.MatchResult
	entries, matchMatchResult = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 2)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "2", entries[1].PeerId)
	assert.Equal(t, "loan", matchMatchResult.Request.ServiceType)
}

func TestFilterAndSortByLevel(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
				Base: 3500,
				Exp:  2,
			},
		},
	}
	var matchMatchResult adapter.MatchResult
	entries, matchMatchResult = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "3", entries[0].PeerId)
	assert.Equal(t, "copy", matchMatchResult.Request.ServiceType)
}

func TestFilterAndSortNoFilters(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[2]},
		{PeerId: "3", Ratio: 0.8, CustomData: dirEntries.Items[4]}}
	var matchMatchResult adapter.MatchResult
	entries, matchMatchResult = ad.FilterAndSort(appCtx, entries, requesterData, nil, nil)
	assert.Len(t, entries, 3)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "3", entries[1].PeerId)
	assert.Equal(t, "2", entries[2].PeerId)
	assert.Equal(t, "", matchMatchResult.Request.ServiceType)
}

func TestCompareSuppliers(t *testing.T) {
	assert.True(t, adapter.CompareSuppliers(adapter.Supplier{}, adapter.Supplier{}) == 0)
	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1}) == 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 0, NetworkPriority: 1, Ratio: 1},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 0, NetworkPriority: 1, Ratio: 1},
		adapter.Supplier{Cost: 1, NetworkPriority: 0, Ratio: 0}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, NetworkPriority: 0, Ratio: 1},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, NetworkPriority: 0, Ratio: 1},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 0}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 0},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 2, NetworkPriority: 1, Ratio: 1},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1}) > 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, NetworkPriority: 2, Ratio: 1},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1}) > 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 2},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1}) > 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1, Local: true},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1, Local: false}) < 0)

	assert.True(t, adapter.CompareSuppliers(
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1, Local: false},
		adapter.Supplier{Cost: 1, NetworkPriority: 1, Ratio: 1, Local: true}) > 0)

	suppliers := []adapter.Supplier{{Cost: 1, NetworkPriority: 1, Ratio: 1, Local: false}, {Cost: 1, NetworkPriority: 1, Ratio: 1, Local: true}}
	slices.SortFunc(suppliers, adapter.CompareSuppliers)
	assert.True(t, suppliers[0].Local) // Local sorted as first
}
