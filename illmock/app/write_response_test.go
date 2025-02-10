package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteHttpResponseWriteFailed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeHttpResponse(w, []byte(strings.Repeat("S1", 160000)))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, err := net.Dial("tcp", server.URL[7:])
	assert.Nil(t, err)
	n, err := conn.Write([]byte("POST / HTTP/1.1\r\nHost: localhost\r\nContent-Type: text/xml\r\n" +
		"Content-Length: 0\r\n\r\n"))
	conn.Close() // close ASAP to trigger write failure
	assert.Nil(t, err)
	assert.Greater(t, n, 20)
}
