package adapter

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/stretchr/testify/require"
)

func TestHostProfileSpecURL(t *testing.T) {
	for _, test := range []struct{ input, expected string }{
		{"https://directory.example/directory/entries", "https://directory.example/directory/openapi.json"},
		{"http://directory.example/entries/", "http://directory.example/openapi.json"},
		{"https://directory.example/prefix%2Fname/entries/", "https://directory.example/prefix%2Fname/openapi.json"},
		{" https://directory.example/prefix/directory/entries/ ,https://other/entries", "https://directory.example/prefix/directory/openapi.json"},
	} {
		actual, err := hostProfileSpecURL(test.input)
		require.NoError(t, err)
		require.Equal(t, test.expected, actual)
	}
	for _, input := range []string{"", ",https://other/entries", "relative/entries", "ftp://example/entries", "https:///entries", "http://example", "http://example/notentries", "http://example/entries?q=1", "http://example/entries#fragment", "http://user:secret@example/entries", "http://example/%invalid"} {
		_, err := hostProfileSpecURL(input)
		require.Error(t, err, input)
	}
}

func TestHostProfileSourceErrorsDoNotExposeCredentials(t *testing.T) {
	_, err := hostProfileSpecURL("http://user:secret@example/%invalid")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	_, err = LoadHostProfiles(common.CreateExtCtxWithArgs(context.Background(), nil), nil, "https://example/entries")
	require.ErrorContains(t, err, "HTTP client is required")
}

func TestLoadHostProfiles(t *testing.T) {
	spec, err := dirapi.GetSpecJSON()
	require.NoError(t, err)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/prefix/directory/openapi.json", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		require.Empty(t, r.Header.Get("X-Okapi-Permissions"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	}))
	defer server.Close()
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	resolver, err := LoadHostProfiles(ctx, server.Client(), server.URL+"/prefix/directory/entries,http://unused.invalid/entries")
	require.NoError(t, err)
	require.Equal(t, 1, requests)
	_, err = resolver.Resolve(dirapi.Entry{})
	require.NoError(t, err)
}

type profileRoundTripper func(*http.Request) (*http.Response, error)

func (f profileRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type profileBody struct {
	io.Reader
	closed bool
}

func (b *profileBody) Close() error { b.closed = true; return nil }

type failedProfileReader struct{ err error }

func (r failedProfileReader) Read([]byte) (int, error) { return 0, r.err }

func TestLoadHostProfilesResponseErrors(t *testing.T) {
	readErr := errors.New("read failed")
	for _, test := range []struct {
		name    string
		status  int
		reader  io.Reader
		message string
	}{
		{"status", http.StatusNotFound, strings.NewReader("secret response"), "HTTP status 404"},
		{"JSON", http.StatusOK, strings.NewReader("invalid"), "decode Directory specification"},
		{"extension", http.StatusOK, strings.NewReader("{}"), "no x-host-profiles"},
		{"oversized", http.StatusOK, io.LimitReader(infiniteProfileReader{}, httpclient.DefaultMaxResponseSize+1), "response body too large"},
		{"read", http.StatusOK, failedProfileReader{readErr}, "read Directory host profiles"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &profileBody{Reader: test.reader}
			attempts := 0
			client := &http.Client{Transport: profileRoundTripper(func(request *http.Request) (*http.Response, error) {
				attempts++
				deadline, ok := request.Context().Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), hostProfileTimeout)
				return &http.Response{StatusCode: test.status, Body: body}, nil
			})}
			_, err := LoadHostProfiles(common.CreateExtCtxWithArgs(context.Background(), nil), client, "https://directory.example/entries")
			require.ErrorContains(t, err, test.message)
			require.Equal(t, 1, attempts)
			require.NotContains(t, err.Error(), "secret response")
			require.True(t, body.closed)
			if test.name == "read" {
				require.ErrorIs(t, err, readErr)
			}
		})
	}
}

type infiniteProfileReader struct{}

func (infiniteProfileReader) Read(data []byte) (int, error) {
	for i := range data {
		data[i] = ' '
	}
	return len(data), nil
}

func TestLoadHostProfilesContextErrors(t *testing.T) {
	for _, test := range []struct {
		name     string
		deadline bool
		expected error
	}{
		{"cancelled", false, context.Canceled},
		{"deadline", true, context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if !test.deadline {
				cancel()
			}
			client := &http.Client{Transport: profileRoundTripper(func(request *http.Request) (*http.Response, error) {
				<-request.Context().Done()
				return nil, request.Context().Err()
			})}
			_, err := LoadHostProfiles(common.CreateExtCtxWithArgs(ctx, nil), client, "https://directory.example/entries")
			require.ErrorIs(t, err, test.expected)
		})
	}
}

func TestLoadHostProfilesRetriesTransientFailures(t *testing.T) {
	spec, err := dirapi.GetSpecJSON()
	require.NoError(t, err)
	for _, test := range []struct {
		name         string
		status       int
		transportErr error
		readErr      error
	}{
		{name: "bad gateway", status: http.StatusBadGateway},
		{name: "unavailable", status: http.StatusServiceUnavailable},
		{name: "gateway timeout", status: http.StatusGatewayTimeout},
		{name: "refused", transportErr: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}},
		{name: "reset", transportErr: syscall.ECONNRESET},
		{name: "DNS", transportErr: &net.DNSError{Err: "temporary lookup failure", IsTemporary: true}},
		{name: "timeout", transportErr: &net.DNSError{Err: "timeout", IsTimeout: true}},
		{name: "EOF", transportErr: io.EOF},
		{name: "interrupted body", readErr: io.ErrUnexpectedEOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			var deadline time.Time
			var bodies []*profileBody
			client := &http.Client{Transport: profileRoundTripper(func(request *http.Request) (*http.Response, error) {
				attempts++
				currentDeadline, ok := request.Context().Deadline()
				require.True(t, ok)
				if attempts == 1 {
					deadline = currentDeadline
				} else {
					require.Equal(t, deadline, currentDeadline)
				}
				for _, body := range bodies {
					require.True(t, body.closed, "previous attempt must close its body")
				}
				if attempts == 1 && test.transportErr != nil {
					return nil, test.transportErr
				}
				status := http.StatusOK
				var reader io.Reader = strings.NewReader(string(spec))
				if attempts == 1 && test.status != 0 {
					status = test.status
					reader = strings.NewReader("secret response")
				}
				if attempts == 1 && test.readErr != nil {
					reader = failedProfileReader{test.readErr}
				}
				body := &profileBody{Reader: reader}
				bodies = append(bodies, body)
				return &http.Response{StatusCode: status, Body: body}, nil
			})}
			resolver, err := LoadHostProfiles(common.CreateExtCtxWithArgs(context.Background(), nil), client, "https://directory.example/entries")
			require.NoError(t, err)
			require.NotNil(t, resolver)
			require.Equal(t, 2, attempts)
			for _, body := range bodies {
				require.True(t, body.closed)
			}
		})
	}
}

func TestLoadHostProfilesDoesNotRetryPermanentFailures(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError} {
		attempts := 0
		client := &http.Client{Transport: profileRoundTripper(func(*http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("secret response"))}, nil
		})}
		_, err := LoadHostProfiles(common.CreateExtCtxWithArgs(context.Background(), nil), client, "https://directory.example/entries")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret response")
		require.Equal(t, 1, attempts)
	}
	for _, transportErr := range []error{x509.UnknownAuthorityError{}, &net.DNSError{Err: "no such host", IsNotFound: true}, errors.New("permanent transport error")} {
		attempts := 0
		client := &http.Client{Transport: profileRoundTripper(func(*http.Request) (*http.Response, error) { attempts++; return nil, transportErr })}
		_, err := LoadHostProfiles(common.CreateExtCtxWithArgs(context.Background(), nil), client, "https://directory.example/entries")
		require.ErrorIs(t, err, transportErr)
		require.Equal(t, 1, attempts)
	}
}

func TestLoadHostProfilesStopsDuringBackoff(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel bool
	}{{"cancelled", true}, {"deadline", false}} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			attempts := 0
			failure := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
			client := &http.Client{Transport: profileRoundTripper(func(request *http.Request) (*http.Response, error) {
				attempts++
				if test.cancel {
					time.AfterFunc(10*time.Millisecond, cancel)
				}
				return nil, failure
			})}
			_, err := LoadHostProfiles(common.CreateExtCtxWithArgs(ctx, nil), client, "https://directory.example/entries")
			if test.cancel {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			require.ErrorIs(t, err, failure)
			require.Equal(t, 1, attempts)
		})
	}
}

func TestLoadHostProfilesPreservesStatusOnRetryExhaustion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	attempts := 0
	client := &http.Client{Transport: profileRoundTripper(func(request *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("secret response"))}, nil
	})}
	_, err := LoadHostProfiles(common.CreateExtCtxWithArgs(ctx, nil), client, "https://directory.example/entries")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "HTTP status 503")
	require.NotContains(t, err.Error(), "secret response")
	require.Equal(t, 2, attempts)
}

func TestLoadHostProfilesHandlesDelayedDirectoryReadiness(t *testing.T) {
	spec, err := dirapi.GetSpecJSON()
	require.NoError(t, err)
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	}))
	defer server.Close()
	resolver, err := LoadHostProfiles(common.CreateExtCtxWithArgs(context.Background(), nil), server.Client(), server.URL+"/directory/entries")
	require.NoError(t, err)
	require.NotNil(t, resolver)
	require.Equal(t, 3, attempts)
}
