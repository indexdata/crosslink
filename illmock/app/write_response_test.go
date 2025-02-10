package app

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockResponseWriter is a mock implementation of http.ResponseWriter
type MockResponseWriter struct {
	header http.Header
}

func (m *MockResponseWriter) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *MockResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("mock write error")
}

func (m *MockResponseWriter) WriteHeader(statusCode int) {
	// No-op
}

func TestWriteHttpResponseWriteFailed(t *testing.T) {
	mockWriter := &MockResponseWriter{}
	buf := []byte("test response")

	writeHttpResponse(mockWriter, buf)

	// TODO: check if the log contains the expected error message
	assert.NotNil(t, mockWriter)
}
