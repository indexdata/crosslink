package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/directory/auth"
	"github.com/stretchr/testify/require"
)

func TestImportBodyLimitRejectsKnownAndChunkedOverflow(t *testing.T) {
	for name, contentLength := range map[string]int64{"known": 129, "chunked": -1} {
		t.Run(name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				called = true
				_, err := io.ReadAll(request.Body)
				if err != nil {
					var maxErr *http.MaxBytesError
					require.ErrorAs(t, err, &maxErr)
					http.Error(w, "too large", http.StatusRequestEntityTooLarge)
				}
			})
			handler := auth.FolioTokenAwareMiddleware(ImportBodyLimitMiddleware(128, next))
			request := httptest.NewRequest(http.MethodPost, BasePath+"/import", strings.NewReader(strings.Repeat("x", 129)))
			request.ContentLength = contentLength
			request.Header.Set(auth.FolioPermissionsHeader, `["directory.consortium.all"]`)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
			if contentLength > 128 {
				require.False(t, called)
			} else {
				require.True(t, called)
			}
		})
	}
}

func TestImportBodyLimitDoesNotReadUnauthorizedOrAffectOtherRoutes(t *testing.T) {
	for name, values := range map[string][2]string{
		"unauthorized": {`["directory.public.all"]`, BasePath + "/import"},
		"other route":  {`["directory.consortium.all"]`, BasePath + "/entries"},
	} {
		t.Run(name, func(t *testing.T) {
			permissions, path := values[0], values[1]
			body := &trackingReadCloser{Reader: strings.NewReader(strings.Repeat("x", 129))}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
			handler := auth.FolioTokenAwareMiddleware(ImportBodyLimitMiddleware(128, next))
			request := httptest.NewRequest(http.MethodPost, path, body)
			request.ContentLength = 129
			request.Header.Set(auth.FolioPermissionsHeader, permissions)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			require.Equal(t, http.StatusUnauthorized, response.Code)
			require.False(t, body.read)
		})
	}
}

func TestImportHandlerRejectsUnauthorizedBeforeValidationReadsBody(t *testing.T) {
	body := &trackingReadCloser{Reader: strings.NewReader("record")}
	request := httptest.NewRequest(http.MethodPost, BasePath+"/import", body)
	request.Header.Set("Content-Type", "application/x-ndjson")
	request.Header.Set(auth.FolioPermissionsHeader, `["directory.public.all"]`)
	response := httptest.NewRecorder()

	InitHandler(context.Background(), nil).ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.False(t, body.read)
}

type trackingReadCloser struct {
	io.Reader
	read bool
}

func (r *trackingReadCloser) Read(data []byte) (int, error) {
	r.read = true
	return r.Reader.Read(data)
}

func (r *trackingReadCloser) Close() error { return nil }
