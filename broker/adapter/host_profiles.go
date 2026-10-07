package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/profiles"
	"github.com/indexdata/crosslink/httpclient"
)

const hostProfileTimeout = 10 * time.Second

// LoadHostProfiles fetches an immutable profile snapshot from the first Directory
// entry URL. Transient fetch failures are retried within a ten-second total
// budget. It also applies when peer lookups use the mock Directory adapter.
func LoadHostProfiles(ctx common.ExtendedContext, client *http.Client, entryURLs string) (*profiles.Resolver, error) {
	if client == nil {
		return nil, fmt.Errorf("Directory profile HTTP client is required")
	}
	specURL, err := hostProfileSpecURL(entryURLs)
	if err != nil {
		return nil, err
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, hostProfileTimeout)
	defer cancel()

	backoff := 100 * time.Millisecond
	var lastErr error
	for {
		if err := ctxTimeout.Err(); err != nil {
			return nil, fmt.Errorf("fetch Directory host profiles: %w", errors.Join(err, lastErr))
		}
		body, retry, err := fetchHostProfiles(ctxTimeout, client, specURL)
		if err == nil {
			resolver, err := profiles.NewResolver(body)
			if err != nil {
				return nil, fmt.Errorf("load Directory host profiles: %w", err)
			}
			return resolver, nil
		}
		lastErr = err
		if ctxTimeout.Err() != nil {
			return nil, fmt.Errorf("fetch Directory host profiles: %w", errors.Join(ctxTimeout.Err(), lastErr))
		}
		if !retry {
			return nil, err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctxTimeout.Done():
			timer.Stop()
			return nil, fmt.Errorf("fetch Directory host profiles: %w", errors.Join(ctxTimeout.Err(), lastErr))
		case <-timer.C:
		}
		backoff = min(backoff*2, time.Second)
	}
}

// fetchHostProfiles performs one attempt and closes its response before returning.
func fetchHostProfiles(ctx context.Context, client *http.Client, specURL string) ([]byte, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create Directory profile request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, transientProfileError(err), fmt.Errorf("fetch Directory host profiles: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		retry := response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusGatewayTimeout
		return nil, retry, fmt.Errorf("fetch Directory host profiles: HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(httpclient.NewLimitErrorReader(response.Body, httpclient.DefaultMaxResponseSize))
	if err != nil {
		return nil, transientProfileError(err), fmt.Errorf("read Directory host profiles: %w", err)
	}
	return body, false, nil
}

func transientProfileError(err error) bool {
	// Unwrap url.Error before checking network errors: its Timeout method also
	// reports context deadlines, which the outer loop handles without retrying.
	var urlError *url.Error
	if errors.As(err, &urlError) {
		err = urlError.Err
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) && dnsError.IsTemporary {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func hostProfileSpecURL(entryURLs string) (string, error) {
	firstURL, _, _ := strings.Cut(entryURLs, ",")
	parsed, err := url.Parse(strings.TrimSpace(firstURL))
	if err != nil {
		// url.Parse errors include the input URL, which may contain credentials.
		if parseError, ok := err.(*url.Error); ok {
			err = parseError.Err
		}
		return "", fmt.Errorf("parse Directory profile source URL: %w", err)
	}
	path := strings.TrimRight(parsed.Path, "/")
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || !strings.HasSuffix(path, "/entries") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", fmt.Errorf("Directory profile source must be an HTTP(S) entries URL without credentials, query, or fragment")
	}
	escapedPath := strings.TrimRight(parsed.EscapedPath(), "/")
	parsed.Path = strings.TrimSuffix(path, "/entries") + "/openapi.json"
	parsed.RawPath = strings.TrimSuffix(escapedPath, "/entries") + "/openapi.json"
	return parsed.String(), nil
}
