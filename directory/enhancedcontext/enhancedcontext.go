package enhancedcontext

import (
	"context"
	"net/http"
)

type OriginalRequestKey string

const originalRequestKey OriginalRequestKey = "ORIGINAL_REQUEST_KEY"

func GetRequest(ctx context.Context) *http.Request {
	requestPtr, ok := ctx.Value(originalRequestKey).(*http.Request)
	if !ok {
		return nil
	}
	return requestPtr
}

func contextWithRequest(ctx context.Context, requestPtr *http.Request) context.Context {
	return context.WithValue(ctx, originalRequestKey, requestPtr)
}

func EnhancedContextMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		ctx = contextWithRequest(ctx, request)

		handler.ServeHTTP(writer, request.WithContext(ctx))
	})
}
