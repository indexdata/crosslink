package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/stretchr/testify/assert"
)

func TestSru500(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("<hello"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, err := ad.Lookup(p)
	assert.ErrorContains(t, err, "500")
}

func TestSruBadXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte("<hello"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, err := ad.Lookup(p)
	assert.ErrorContains(t, err, "decoding failed")
}
