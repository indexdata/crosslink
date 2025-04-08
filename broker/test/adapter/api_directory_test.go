package adapter

import (
	"encoding/json"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookup400(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("Invalid request"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := adapter.CreateApiDirectory(http.DefaultClient, server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	_, err := ad.Lookup(p)
	assert.ErrorContains(t, err, "400")
}

func TestLookupInvalidUrl(t *testing.T) {
	ad := adapter.CreateApiDirectory(http.DefaultClient, "invalid")
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	_, err := ad.Lookup(p)
	assert.ErrorContains(t, err, "unsupported protocol scheme")
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

	ad := adapter.CreateApiDirectory(http.DefaultClient, server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err := ad.Lookup(p)
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

	ad := adapter.CreateApiDirectory(http.DefaultClient, server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err := ad.Lookup(p)
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

	ad := adapter.CreateApiDirectory(http.DefaultClient, server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 0)
}

func TestLookup(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		respBody := "{\"items\":[{\"endpoints\":[{\"address\":\"http://localhost:8081/directory\"}]," +
			"\"name\":\"Peer\",\"symbols\":[{\"authority\":\"ISIL\",\"symbol\":\"PEER\"},{\"authority\":\"ZFL\",\"symbol\":\"PEER\"}]}]," +
			"\"resultInfo\":{\"totalRecords\":1}}"
		w.Write([]byte(respBody))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := adapter.CreateApiDirectory(http.DefaultClient, server.URL)
	p := adapter.DirectoryLookupParams{
		Symbols: []string{"ISIL:PEER"},
	}
	entries, err := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, entries[0].Name, "Peer")
	assert.Len(t, entries[0].Symbol, 2)
}

func TestFilterAndSort(t *testing.T) {
	requesterData := map[string]any{}
	requesterData["networks"] = []map[string]any{{"name": "NSW & ACT", "priority": 1}, {"name": "Victoria", "priority": 2}}
	supData1 := map[string]any{}
	supData1["networks"] = []map[string]any{{"name": "NSW & ACT", "priority": 1}, {"name": "Victoria", "priority": 2}}
	supData1["tiers"] = []map[string]any{{"name": "Reciprocal Peer to Peer - Core", "services": []map[string]any{{"cost": 0, "level": "Core", "type": "Loan"}, {"cost": 0, "level": "Core", "type": "Copy"}}}, {"name": "Premium Pay for Peer - Core", "services": []map[string]any{{"cost": 34.4, "level": "Core", "type": "Loan"}, {"cost": 34.4, "level": "Core", "type": "Copy"}}}}
	supData2 := map[string]any{}
	supData2["networks"] = []map[string]any{{"name": "NSW & ACT", "priority": 1}, {"name": "Victoria", "priority": 2}}
	supData2["tiers"] = []map[string]any{{"name": "Reciprocal Peer to Peer - Core", "services": []map[string]any{{"cost": 1, "level": "Core", "type": "Loan"}, {"cost": 1, "level": "Core", "type": "Copy"}}}, {"name": "Premium Pay for Peer - Core", "services": []map[string]any{{"cost": 34.4, "level": "Core", "type": "Loan"}, {"cost": 34.4, "level": "Core", "type": "Copy"}}}}
	entries := []adapter.SupplierToAdd{{PeerId: "1", Ratio: 0.5, CustomData: supData1}, {PeerId: "2", Ratio: 0.7, CustomData: supData2}}
	serviceInfo := iso18626.ServiceInfo{
		ServiceLevel: &iso18626.TypeSchemeValuePair{
			Text: "Core",
		},
		ServiceType: iso18626.TypeServiceTypeLoan,
	}
	billingInfo := iso18626.BillingInfo{
		MaximumCosts: &iso18626.TypeCosts{
			MonetaryValue: utils.XSDDecimal{
				Base: 1150,
				Exp:  2,
			},
		},
	}
	ad := adapter.CreateApiDirectory(http.DefaultClient, "")
	entries = ad.FilterAndSort(entries, requesterData, &serviceInfo, &billingInfo)
	assert.Equal(t, "1", entries[0].PeerId)
}
