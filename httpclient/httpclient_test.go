package httpclient

import (
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/indexdata/crosslink/illmock/testutil"
	"github.com/stretchr/testify/assert"
)

type myType struct {
	Msg string `xml:"msg"`
}

func TestBadScheme(t *testing.T) {
	var response myType
	err := GetXml(http.DefaultClient, "xxx:/", &response)
	assert.ErrorContains(t, err, "unsupported protocol scheme")
}

func TestBadUrlChar(t *testing.T) {
	var response myType
	err := GetXml(http.DefaultClient, "http://localhost:8081\x7f", response)
	assert.ErrorContains(t, err, "invalid control character in URL")
}

func TestBadConnectionRefused(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	assert.Nil(t, err)
	l, err := net.ListenTCP("tcp", addr)
	assert.Nil(t, err)
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	var request, response myType
	err = PostXml(http.DefaultClient, "http://localhost:"+port, request, &response)
	assert.ErrorContains(t, err, "connection refused")
}

func TestServerForbidden(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	var request, response myType
	err := PostXml(http.DefaultClient, server.URL, request, &response)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "HTTP error: 403")
	httpErr, ok := err.(*HttpError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusForbidden, httpErr.StatusCode)
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
	var request, response myType
	err := PostXml(http.DefaultClient, server.URL, request, &response)
	assert.ErrorContains(t, err, "application/xml")
	assert.ErrorContains(t, err, "text/xml")
}

func TestPostXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/xml", r.Header.Get("Content-Type"))
		buf, err := io.ReadAll(r.Body)
		assert.Nil(t, err)
		assert.NotNil(t, buf)
		var request myType
		err = xml.Unmarshal(buf, &request)
		assert.Nil(t, err)
		assert.Equal(t, "hello", request.Msg)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		var response myType
		response.Msg = "world"
		output, err := xml.Marshal(response)
		assert.Nil(t, err)
		_, err = w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	var request, response myType
	request.Msg = "hello"
	err := PostXml(http.DefaultClient, server.URL, request, &response)
	assert.Nil(t, err)
	assert.Equal(t, "world", response.Msg)
}

func TestServerApplicationXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "application/xml", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "Application/XML; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		var response myType
		response.Msg = "world"
		output, err := xml.Marshal(response)
		assert.Nil(t, err)
		_, err = w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	var response myType
	err := GetXml(http.DefaultClient, server.URL, &response)
	assert.Nil(t, err)
	assert.Equal(t, "world", response.Msg)
}

func TestServerTextXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "application/xml", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		var response myType
		response.Msg = "world"
		output, err := xml.Marshal(response)
		assert.Nil(t, err)
		_, err = w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	var response myType
	err := GetXml(http.DefaultClient, server.URL, &response)
	assert.Nil(t, err)
	assert.Equal(t, "world", response.Msg)
}

func TestServerBrokenPipe(t *testing.T) {
	l := testutil.GetFreeListener(t)
	url := "http://localhost:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	go func() {
		conn, err := l.Accept()
		assert.Nil(t, err)
		conn = conn.(*net.TCPConn)
		defer conn.Close()
		var buf [100]byte
		n, err := conn.Read(buf[:])
		assert.Nil(t, err)
		assert.Greater(t, n, 10)
		// length is 2 but only 1 byte sent
		n, err = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nContent-Type: text/xml\r\n\r\nX"))
		assert.Nil(t, err)
		assert.Greater(t, n, 20)
	}()
	var request, response myType
	err := PostXml(http.DefaultClient, url, request, &response)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "read: connection reset by peer")
}

func TestMarshalFailed(t *testing.T) {
	var request, response myType
	marshal := func(v any) ([]byte, error) {
		return nil, fmt.Errorf("foo")
	}
	err := requestResponse(http.DefaultClient, http.MethodGet, []string{"text/plain"}, "http://localhost:9999", request, response, marshal, xml.Unmarshal)
	assert.ErrorContains(t, err, "marshal failed: foo")
}

func TestCustomHeader(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "tenant", r.Header.Get("X-Okapi-Tenant"))
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("<myType><msg>OK</msg></myType>"))
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	var response myType
	Headers.Set("X-Okapi-Tenant", "tenant")
	err := GetXml(http.DefaultClient, server.URL, &response)
	assert.Nil(t, err)
	assert.Equal(t, "OK", response.Msg)
	Headers.Del("X-Okapi-Tenant")
}
