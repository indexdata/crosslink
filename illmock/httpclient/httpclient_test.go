package httpclient

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestNilMsg(t *testing.T) {
	_, err := SendReceiveDefault("http://localhost:8081", nil)
	assert.ErrorContains(t, err, "marshal failed")
}

func TestBadScheme(t *testing.T) {
	_, err := SendReceiveDefault("xxx:/", &iso18626.Iso18626MessageNS{})
	assert.ErrorContains(t, err, "unsupported protocol scheme")
}

func TestBadUrlChar(t *testing.T) {
	_, err := SendReceiveDefault("http://localhost:8081\x7f", &iso18626.Iso18626MessageNS{})
	assert.ErrorContains(t, err, "invalid control character in URL")
}

func TestBadConnectionRefused(t *testing.T) {
	_, err := SendReceiveDefault("http://localhost:8099", &iso18626.Iso18626MessageNS{})
	assert.ErrorContains(t, err, "connection refused")
}

func TestServerForbidden(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	_, err := SendReceiveDefault(server.URL, &iso18626.Iso18626MessageNS{})
	assert.ErrorContains(t, err, "HTTP POST error: 403")
}

func TestServerBadContentType(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		var output []byte
		_, err := w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	_, err := SendReceiveDefault(server.URL, &iso18626.Iso18626MessageNS{})
	assert.ErrorContains(t, err, "application/xml or text/xml")
}

func TestServerBadXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		output := []byte("<x<y>")
		_, err := w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	_, err := SendReceiveDefault(server.URL, &iso18626.Iso18626MessageNS{})
	assert.ErrorContains(t, err, "XML syntax error on line 1")
}

func TestServerApplicationXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		output, _ := xml.Marshal(&iso18626.Iso18626MessageNS{})
		_, err := w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	msg, err := SendReceiveDefault(server.URL, &iso18626.Iso18626MessageNS{})
	assert.Nil(t, err)
	assert.Nil(t, msg.Request)
}

func TestServerTextXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		output, _ := xml.Marshal(&iso18626.Iso18626MessageNS{})
		_, err := w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	msg, err := SendReceiveDefault(server.URL, &iso18626.Iso18626MessageNS{})
	assert.Nil(t, err)
	assert.Nil(t, msg.Request)
}
