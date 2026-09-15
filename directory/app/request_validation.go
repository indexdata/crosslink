package app

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	apiValidator "github.com/oapi-codegen/nethttp-middleware"
)

const importContentType = "application/x-ndjson"

func openAPIRequestValidationMiddleware(spec *openapi3.T) func(http.Handler) http.Handler {
	genericValidator := apiValidator.OapiRequestValidatorWithOptions(spec, &apiValidator.Options{
		Skipper: isImportRequest,
	})
	return func(next http.Handler) http.Handler {
		return validateImportRequest(genericValidator(next))
	}
}

func validateImportRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !isImportRequest(request) {
			next.ServeHTTP(writer, request)
			return
		}

		contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || contentType != importContentType {
			http.Error(writer, "invalid Content-Type: expected "+importContentType, http.StatusBadRequest)
			return
		}
		if request.Body == nil || request.Body == http.NoBody {
			http.Error(writer, "body is required", http.StatusBadRequest)
			return
		}

		var firstByte [1]byte
		if _, err := io.ReadFull(request.Body, firstByte[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				http.Error(writer, "body is required", http.StatusBadRequest)
			} else {
				http.Error(writer, "failed to read request body", http.StatusBadRequest)
			}
			return
		}
		request.Body = &prefixedReadCloser{
			Reader: io.MultiReader(bytes.NewReader(firstByte[:]), request.Body),
			Closer: request.Body,
		}
		next.ServeHTTP(writer, request)
	})
}

func isImportRequest(request *http.Request) bool {
	return request.Method == http.MethodPost && request.URL.Path == BasePath+"/import"
}

type prefixedReadCloser struct {
	io.Reader
	io.Closer
}
