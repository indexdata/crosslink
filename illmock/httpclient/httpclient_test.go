package httpclient

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBadScheme(t *testing.T) {
	_, err := SendReceiveXml(http.DefaultClient, "xxx:/", nil)
	assert.ErrorContains(t, err, "unsupported protocol scheme")
}

func TestBadUrlChar(t *testing.T) {
	_, err := SendReceiveXml(http.DefaultClient, "http://localhost:8081\x7f", nil)
	assert.ErrorContains(t, err, "invalid control character in URL")
}

func TestBadConnectionRefused(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	assert.Nil(t, err)
	port := strconv.Itoa(addr.Port)
	_, err = SendReceiveXml(http.DefaultClient, "http://localhost:"+port, nil)
	assert.ErrorContains(t, err, "connection refused")
}

func TestServerForbidden(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	_, err := SendReceiveXml(http.DefaultClient, server.URL, nil)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "HTTP POST error: 403")
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
	_, err := SendReceiveXml(http.DefaultClient, server.URL, nil)
	assert.ErrorContains(t, err, "application/xml or text/xml")
}

func TestServerApplicationXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		output := []byte("<x/>")
		_, err := w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	msg, err := SendReceiveXml(http.DefaultClient, server.URL, nil)
	assert.Nil(t, err)
	assert.NotNil(t, msg)
}

func TestServerTextXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		output := []byte("<x/>")
		_, err := w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	msg, err := SendReceiveXml(http.DefaultClient, server.URL, nil)
	assert.Nil(t, err)
	assert.NotNil(t, msg)
}

func TestServerBrokenPipe(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	assert.Nil(t, err)
	l, err := net.ListenTCP("tcp", addr)
	assert.Nil(t, err)
	defer l.Close()
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
	_, err = SendReceiveXml(http.DefaultClient, url, nil)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "read: connection reset by peer")
}
