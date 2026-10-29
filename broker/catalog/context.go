package catalog

import (
	"context"
	"io"
	"net/http"
)

// LookupContext performs a lookup with cancellation when supported by the adapter.
// Legacy and test adapters retain the Lookup contract.
func LookupContext(ctx context.Context, adapter LookupAdapter, params LookupParams) (LookupResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a, ok := adapter.(interface {
		LookupContext(context.Context, LookupParams) (LookupResult, error)
	}); ok {
		return a.LookupContext(ctx, params)
	}
	return adapter.Lookup(params)
}

type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Keep both the caller cancellation and http.Client timeout alive through body reads.
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	if err := t.ctx.Err(); err != nil {
		cleanup()
		return nil, err
	}
	response, err := t.base.RoundTrip(r.Clone(ctx))
	if err != nil {
		cleanup()
		return nil, err
	}
	response.Body = &contextBody{ReadCloser: response.Body, cleanup: cleanup}
	return response, nil
}

type contextBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *contextBody) Close() error {
	defer b.cleanup()
	return b.ReadCloser.Close()
}

// LookupContext uses a per-call client copy so shared adapters remain safe for concurrent callers.
func (s *SruLookupAdapter) LookupContext(ctx context.Context, params LookupParams) (LookupResult, error) {
	copyAdapter := *s
	client := *s.client
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = contextTransport{ctx: ctx, base: transport}
	copyAdapter.client = &client
	return copyAdapter.Lookup(params)
}

func (a *MetaproxyLookupAdapter) LookupContext(ctx context.Context, params LookupParams) (LookupResult, error) {
	return LookupContext(ctx, a.holdingsLookupAdapter, params)
}
