package app

import (
	"net/http"

	"github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/directory/auth"
)

const MaxImportBodyBytes int64 = 1 << 30

func ImportBodyLimitMiddleware(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != BasePath+"/import" {
			next.ServeHTTP(writer, request)
			return
		}
		authData := auth.GetAuthData(request.Context())
		if authData == nil || !authData.HasRole(auth.ConsortialAdminRole) {
			api.WriteError(writer, http.StatusForbidden, "Access denied")
			return
		}
		if request.ContentLength > maxBytes {
			api.WriteError(writer, http.StatusRequestEntityTooLarge, "import request too large")
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, maxBytes)
		next.ServeHTTP(writer, request)
	})
}
