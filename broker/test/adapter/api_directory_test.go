package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/test"
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

	ad := createDirectoryAdapter(server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err, _ := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 3)
	assert.Equal(t, entries[0].Name, "Albury City Libraries")
	assert.Len(t, entries[0].Symbol, 1)

	ad = createDirectoryAdapter(server.URL, server.URL)
	p = adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err, _ = ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 6)
	assert.Equal(t, entries[0].Name, "Albury City Libraries")
	assert.Len(t, entries[0].Symbol, 1)
}

func TestFilterAndSort(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[1]},
		{PeerId: "3", Ratio: 0.7, CustomData: dirEntries.Items[2]}}
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
	entries = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 3)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "3", entries[1].PeerId)
	assert.Equal(t, "2", entries[2].PeerId)
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
	entries = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "1", entries[0].PeerId)
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
	entries = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "1", entries[0].PeerId)
}

func TestFilterAndSortByType(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[1]},
		{PeerId: "3", Ratio: 0.7, CustomData: dirEntries.Items[2]}}
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
	entries = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 2)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "2", entries[1].PeerId)
}

func TestFilterAndSortByLevel(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[1]},
		{PeerId: "3", Ratio: 0.7, CustomData: dirEntries.Items[2]}}
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
	entries = ad.FilterAndSort(appCtx, entries, requesterData, &serviceInfo, &billingInfo)
	assert.Len(t, entries, 1)
	assert.Equal(t, "3", entries[0].PeerId)
}

func TestFilterAndSortNoFilters(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	ad := createDirectoryAdapter("")
	requesterData := dirEntries.Items[0]
	entries := []adapter.Supplier{
		{PeerId: "1", Ratio: 0.5, CustomData: dirEntries.Items[0]},
		{PeerId: "2", Ratio: 0.7, CustomData: dirEntries.Items[1]},
		{PeerId: "3", Ratio: 0.8, CustomData: dirEntries.Items[2]}}
	entries = ad.FilterAndSort(appCtx, entries, requesterData, nil, nil)
	assert.Len(t, entries, 3)
	assert.Equal(t, "1", entries[0].PeerId)
	assert.Equal(t, "3", entries[1].PeerId)
	assert.Equal(t, "2", entries[2].PeerId)
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
}
