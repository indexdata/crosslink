package common

import (
	"context"
	"net/http"
)

type httpRequestCtxKey struct{}

func WithHTTPRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, httpRequestCtxKey{}, r)
}

func HTTPRequestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(httpRequestCtxKey{}).(*http.Request)
	return r, ok
}
