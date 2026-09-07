package app

import (
	"net/http"

	"github.com/indexdata/crosslink/directory/auth"
)

const MaxImportBodyBytes int64 = 2 << 30

func ImportBodyLimitMiddleware(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != BasePath+"/import" {
			next.ServeHTTP(writer, request)
			return
		}
		authData := auth.GetAuthData(request.Context())
		if authData == nil || !authData.HasRole(auth.ConsortialAdminRole) {
			http.Error(writer, "Access denied", http.StatusUnauthorized)
			return
		}
		if request.ContentLength > maxBytes {
			http.Error(writer, "import request too large", http.StatusRequestEntityTooLarge)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, maxBytes)
		next.ServeHTTP(writer, request)
	})
}
