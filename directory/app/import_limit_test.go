package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/directory/auth"
	"github.com/stretchr/testify/require"
)

func TestOpenAPIRequestValidationStreamsImportBody(t *testing.T) {
	spec, err := api.GetSpec()
	require.NoError(t, err)
	body := &countingBody{remaining: MaxImportBodyBytes}
	request := httptest.NewRequest(http.MethodPost, BasePath+"/import", body)
	request.ContentLength = MaxImportBodyBytes
	request.Header.Set("Content-Type", "application/x-ndjson")
	response := httptest.NewRecorder()
	handler := openAPIRequestValidationMiddleware(spec)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.EqualValues(t, 1, body.bytesRead)
}

func TestOpenAPIRequestValidationReplaysCompleteImportBody(t *testing.T) {
	spec, err := api.GetSpec()
	require.NoError(t, err)
	const payload = "first record\nsecond record\n"
	var received string
	handler := openAPIRequestValidationMiddleware(spec)(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)
		received = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, BasePath+"/import", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/x-ndjson; charset=utf-8")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, payload, received)
}

func TestOpenAPIRequestValidationRetainsImportBodyChecks(t *testing.T) {
	spec, err := api.GetSpec()
	require.NoError(t, err)
	for name, testCase := range map[string]struct {
		body        io.Reader
		contentType string
	}{
		"missing body":       {body: http.NoBody, contentType: "application/x-ndjson"},
		"wrong content type": {body: strings.NewReader("record"), contentType: "application/json"},
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			handler := openAPIRequestValidationMiddleware(spec)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))
			request := httptest.NewRequest(http.MethodPost, BasePath+"/import", testCase.body)
			request.Header.Set("Content-Type", testCase.contentType)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			require.Equal(t, http.StatusBadRequest, response.Code)
			require.False(t, called)
		})
	}
}

func TestOpenAPIRequestValidationReportsExpectedImportContentType(t *testing.T) {
	spec, err := api.GetSpec()
	require.NoError(t, err)
	handler := openAPIRequestValidationMiddleware(spec)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := httptest.NewRequest(http.MethodPost, BasePath+"/import", strings.NewReader("record"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, "invalid Content-Type: expected application/x-ndjson\n", response.Body.String())
}

func TestOpenAPIRequestValidationStillValidatesOtherRequestBodies(t *testing.T) {
	spec, err := api.GetSpec()
	require.NoError(t, err)
	called := false
	handler := openAPIRequestValidationMiddleware(spec)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodPost, BasePath+"/entries", http.NoBody)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.False(t, called)
}

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

type countingBody struct {
	remaining int64
	bytesRead int64
}

func (r *countingBody) Read(data []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	count := min(int64(len(data)), r.remaining)
	for index := range int(count) {
		data[index] = 'x'
	}
	r.remaining -= count
	r.bytesRead += count
	return int(count), nil
}

func (r *countingBody) Close() error { return nil }
