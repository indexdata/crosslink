package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testUUID = "d8290e68-bfbb-3bc8-b621-5a9590aa29fd"

func multipartBody(t *testing.T, size int, extra bool) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	field, err := writer.CreateFormFile("file", "document.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(field, strings.NewReader(strings.Repeat("x", size)), int64(size)); err != nil {
		t.Fatal(err)
	}
	if extra {
		if err := writer.WriteField("extra", strings.Repeat("y", size)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func storageServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("MOD_DMS_BUCKET", "documents,us-east-1,"+strings.TrimPrefix(server.URL, "http://")+",access,secret")
	t.Setenv("MOD_DMS_INSECURE", "true")
}

func TestUploadLimit(t *testing.T) {
	var calls atomic.Int32
	storageServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("ETag", `"abc"`)
	})
	body, contentType := multipartBody(t, 32, false)
	for _, unknown := range []bool{false, true} {
		for _, delta := range []int{-1, 0, 1} {
			t.Run(strconv.FormatBool(unknown)+"/"+strconv.Itoa(delta), func(t *testing.T) {
				t.Setenv("MOD_DMS_MAX_UPLOAD_BYTES", strconv.Itoa(len(body)+delta))
				req := httptest.NewRequest(http.MethodPost, "/dms/upload", bytes.NewReader(body))
				if unknown {
					req.ContentLength = -1
				}
				req.Header.Set("Content-Type", contentType)
				before := calls.Load()
				response := httptest.NewRecorder()
				Handler(context.Background()).ServeHTTP(response, req)
				want := http.StatusOK
				if delta < 0 {
					want = http.StatusRequestEntityTooLarge
				}
				if response.Code != want {
					t.Fatalf("status %d, want %d: %s", response.Code, want, response.Body.String())
				}
				if delta < 0 && calls.Load() != before {
					t.Fatal("oversized request contacted storage")
				}
				if delta >= 0 {
					if response.Header().Get("Content-Type") != "application/json" {
						t.Fatal("missing JSON content type")
					}
					var uploaded Uploaded
					if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
						t.Fatal(err)
					}
					if !validKey(uploaded.Key, "") || uploaded.Url == "" {
						t.Fatalf("unexpected response: %+v", uploaded)
					}
				}
			})
		}
	}
	large, ct := multipartBody(t, 1024, true)
	t.Setenv("MOD_DMS_MAX_UPLOAD_BYTES", "1500")
	req := httptest.NewRequest(http.MethodPost, "/dms/upload", bytes.NewReader(large))
	req.Header.Set("Content-Type", ct)
	response := httptest.NewRecorder()
	before := calls.Load()
	Handler(context.Background()).ServeHTTP(response, req)
	if response.Code != 413 || calls.Load() != before {
		t.Fatal("extra multipart part escaped the cap")
	}
}

func TestUploadConfigurationAndMultipartErrors(t *testing.T) {
	storageServer(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request contacted storage") })
	for _, value := range []string{"", "0", "-1", "abc", "9223372036854775808"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MOD_DMS_MAX_UPLOAD_BYTES", value)
			if _, err := uploadLimit(); err == nil {
				t.Fatal("accepted invalid limit")
			}
			res := httptest.NewRecorder()
			Handler(context.Background()).ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/dms/upload", nil))
			if res.Code != 503 {
				t.Fatalf("status %d", res.Code)
			}
		})
	}
	for _, tc := range []struct {
		contentType string
		status      int
	}{
		{"multipart/form-data", 400},
		{"multipart/form-data; boundary=missing", 400},
		{"Multipart/Form-Data; boundary=missing", 400},
		{"text/plain", 415},
		{"", 415},
		{"multipart/form-dataevil; boundary=missing", 415},
		{"multipart/form-data; boundary=\"unterminated", 415},
	} {
		req := httptest.NewRequest(http.MethodPost, "/dms/upload", strings.NewReader("bad"))
		req.Header.Set("Content-Type", tc.contentType)
		res := httptest.NewRecorder()
		Handler(context.Background()).ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("content type %q: status %d, want %d", tc.contentType, res.Code, tc.status)
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("other", "value"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/dms/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	Handler(context.Background()).ServeHTTP(res, req)
	if res.Code != 400 {
		t.Fatalf("missing file: %d", res.Code)
	}
}

func TestKeyValidation(t *testing.T) {
	var calls atomic.Int32
	storageServer(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) })
	cases := []struct {
		key, tenant string
		valid       bool
	}{
		{testUUID, "", true}, {"tenant/" + testUUID, "tenant", true}, {"tenant/" + testUUID, "", false},
		{testUUID, "tenant", false}, {"other/" + testUUID, "tenant", false}, {"tenant/extra/" + testUUID, "tenant", false},
		{"tenant/" + testUUID + "/", "tenant", false}, {"tenant//" + testUUID, "tenant", false}, {"invalid", "", false},
		{"tenant%2Fextra/" + testUUID, "tenant", false}, {"tenant/" + testUUID, "bad/tenant", false},
	}
	for _, tc := range cases {
		t.Run(tc.key+"/"+tc.tenant, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/dms/upload/"+tc.key, nil)
			req.Header.Set("X-Okapi-Tenant", tc.tenant)
			res := httptest.NewRecorder()
			before := calls.Load()
			Handler(context.Background()).ServeHTTP(res, req)
			want := 400
			if tc.valid {
				want = 200
			}
			if res.Code != want {
				t.Fatalf("status %d, want %d", res.Code, want)
			}
			if !tc.valid && calls.Load() != before {
				t.Fatal("invalid key contacted storage")
			}
		})
	}
	for _, tenant := range []string{".", "..", "bad/tenant", `bad\tenant`, "bad tenant", "bad\t", "bad\x00"} {
		if validTenant(tenant) {
			t.Fatalf("accepted tenant %q", tenant)
		}
		body, ct := multipartBody(t, 1, false)
		req := httptest.NewRequest(http.MethodPost, "/dms/upload", bytes.NewReader(body))
		req.Header.Set("Content-Type", ct)
		req.Header.Set("X-Okapi-Tenant", tenant)
		res := httptest.NewRecorder()
		before := calls.Load()
		Handler(context.Background()).ServeHTTP(res, req)
		if res.Code != 400 || calls.Load() != before {
			t.Fatalf("invalid upload tenant %q: %d", tenant, res.Code)
		}
	}
}

func TestStorageFailuresAndTemporaryCleanup(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	storageServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, "denied", http.StatusForbidden)
	})
	body, ct := multipartBody(t, 33<<20, false)
	req := httptest.NewRequest(http.MethodPost, "/dms/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	res := httptest.NewRecorder()
	Handler(context.Background()).ServeHTTP(res, req)
	if res.Code != 503 || strings.Contains(res.Body.String(), "url") {
		t.Fatalf("failed upload: %d %s", res.Code, res.Body.String())
	}
	assertEmptyDir(t, temp)
	req = httptest.NewRequest(http.MethodDelete, "/dms/upload/"+testUUID, nil)
	res = httptest.NewRecorder()
	Handler(context.Background()).ServeHTTP(res, req)
	if res.Code != 503 {
		t.Fatalf("failed delete: %d", res.Code)
	}
}

func TestHandlerReusesStorageClient(t *testing.T) {
	var connections, calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.Header().Set("ETag", `"abc"`)
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	t.Setenv("MOD_DMS_BUCKET", "documents,us-east-1,"+strings.TrimPrefix(server.URL, "http://")+",access,secret")
	t.Setenv("MOD_DMS_INSECURE", "true")
	handler := Handler(context.Background())
	// Routes keep their startup configuration, even if the environment changes.
	t.Setenv("MOD_DMS_BUCKET", "invalid")
	for range 2 {
		body, contentType := multipartBody(t, 1, false)
		req := httptest.NewRequest(http.MethodPost, "/dms/upload", bytes.NewReader(body))
		req.Header.Set("Content-Type", strings.Replace(contentType, "multipart/form-data", "Multipart/Form-Data", 1))
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("upload: %d %s", res.Code, res.Body.String())
		}
		var uploaded Uploaded
		if err := json.Unmarshal(res.Body.Bytes(), &uploaded); err != nil {
			t.Fatal(err)
		}
		res = httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodDelete, "/dms/upload/"+uploaded.Key, nil))
		if res.Code != http.StatusOK {
			t.Fatalf("delete: %d %s", res.Code, res.Body.String())
		}
	}
	if calls.Load() != 4 || connections.Load() != 1 {
		t.Fatalf("storage calls %d, connections %d; want four calls sharing one connection", calls.Load(), connections.Load())
	}
}

func TestStorageCancellation(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			started := make(chan struct{})
			stopped := make(chan struct{})
			storageServer(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				<-r.Context().Done()
				close(stopped)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(method, "/dms/upload/"+testUUID, nil)
			if method == http.MethodPost {
				body, ct := multipartBody(t, 1, false)
				req = httptest.NewRequest(method, "/dms/upload", bytes.NewReader(body))
				req.Header.Set("Content-Type", ct)
			}
			req = req.WithContext(ctx)
			res := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { Handler(context.Background()).ServeHTTP(res, req); close(done) }()
			await(t, started)
			cancel()
			await(t, done)
			await(t, stopped)
			if res.Code != 503 {
				t.Fatalf("canceled storage: %d", res.Code)
			}
		})
	}
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestServeLifecycle(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(strconv.FormatBool(force), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			release := make(chan struct{})
			canceled := make(chan struct{})
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					close(canceled)
				}
			})}
			timeout := time.Second
			if force {
				timeout = 20 * time.Millisecond
			}
			done := make(chan error, 1)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			go func() { done <- serveListener(ctx, server, listener, timeout) }()
			clientDone := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: 3 * time.Second}
				res, err := client.Get("http://" + address)
				if res != nil {
					_ = res.Body.Close()
				}
				clientDone <- err
			}()

			shutdownStarted := make(chan struct{})
			server.RegisterOnShutdown(func() { close(shutdownStarted) })
			await(t, entered)
			cancel()
			await(t, shutdownStarted)
			conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				t.Fatal("shutdown accepted a new connection")
			}
			if !force {
				select {
				case <-canceled:
					t.Fatal("draining canceled request")
				default:
				}
				close(release)
			}
			select {
			case err := <-done:
				if force && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("shutdown: %v", err)
				}
				if !force && err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("server did not stop")
			}
			select {
			case err := <-clientDone:
				if !force && err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("client did not finish")
			}
			if force {
				await(t, canceled)
			}
			conn, err = net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				t.Fatal("listener still open")
			}
		})
	}
}

func TestServeBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if err := serve(context.Background(), &http.Server{Addr: listener.Addr().String()}, time.Second); err == nil {
		t.Fatal("expected bind failure")
	}
}

func assertEmptyDir(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files remain: %v", entries)
	}
}

func TestStartConfiguration(t *testing.T) {
	t.Setenv("LOG_LEVEL", "invalid")
	if err := Start(context.Background()); err == nil {
		t.Fatal("invalid logger accepted")
	}
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("MOD_DMS_BUCKET", "")
	if err := Start(context.Background()); err == nil {
		t.Fatal("invalid bucket accepted")
	}
	t.Setenv("MOD_DMS_BUCKET", "bucket,us-east-1,https://localhost:9000/path,access,secret")
	if err := Start(context.Background()); err == nil || !strings.Contains(err.Error(), "configure bucket") {
		t.Fatalf("invalid endpoint: %v", err)
	}
	res := httptest.NewRecorder()
	Handler(context.Background()).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("invalid endpoint health status: %d", res.Code)
	}
	t.Setenv("MOD_DMS_BUCKET", "bucket,us-east-1,localhost:9000,access,secret")
	t.Setenv("MOD_DMS_MAX_UPLOAD_BYTES", "0")
	if err := Start(context.Background()); err == nil {
		t.Fatal("invalid limit accepted")
	}
}

func TestUploadEpilogueLimit(t *testing.T) {
	var calls atomic.Int32
	storageServer(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	body, ct := multipartBody(t, 1, false)
	t.Setenv("MOD_DMS_MAX_UPLOAD_BYTES", strconv.Itoa(len(body)+5000))
	body = append(body, bytes.Repeat([]byte("x"), 10000)...)
	req := httptest.NewRequest(http.MethodPost, "/dms/upload", bytes.NewReader(body))
	req.ContentLength = -1
	req.Header.Set("Content-Type", ct)
	res := httptest.NewRecorder()
	Handler(context.Background()).ServeHTTP(res, req)
	if res.Code != 413 || calls.Load() != 0 {
		t.Fatalf("epilogue bypassed limit: %d", res.Code)
	}
}

func TestTemporaryCleanupOnRejection(t *testing.T) {
	storageServer(t, func(w http.ResponseWriter, r *http.Request) { t.Error("rejected request contacted storage") })
	body, ct := multipartBody(t, 33<<20, false)
	for _, oversized := range []bool{false, true} {
		t.Run(strconv.FormatBool(oversized), func(t *testing.T) {
			temp := t.TempDir()
			t.Setenv("TMPDIR", temp)
			t.Setenv("MOD_DMS_TYPES", "application/pdf")
			want := http.StatusUnsupportedMediaType
			if oversized {
				t.Setenv("MOD_DMS_MAX_UPLOAD_BYTES", strconv.Itoa(len(body)-1))
				want = http.StatusRequestEntityTooLarge
			}
			req := httptest.NewRequest(http.MethodPost, "/dms/upload", bytes.NewReader(body))
			req.Header.Set("Content-Type", ct)
			res := httptest.NewRecorder()
			Handler(context.Background()).ServeHTTP(res, req)
			if res.Code != want {
				t.Fatalf("status %d, want %d", res.Code, want)
			}
			assertEmptyDir(t, temp)
		})
	}
}

func TestCredsComponentCount(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"missing", "", false},
		{"four", "bucket,region,endpoint,access", false},
		{"five", "bucket,region,endpoint,access,secret", true},
		{"empty region", "bucket,,endpoint,access,secret", true},
		{"six", "bucket,region,endpoint,access,secret,extra", false},
		{"comma in secret", "bucket,region,endpoint,access,secret,with,commas", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MOD_DMS_BUCKET", tc.value)
			creds, err := Creds()
			if !tc.valid {
				if err == nil {
					t.Fatal("accepted invalid component count")
				}
				if !strings.Contains(err.Error(), "exactly five") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "access") {
					t.Fatalf("unexpected configuration error: %v", err)
				}
				if creds != (BucketCreds{}) {
					t.Fatal("returned partial credentials")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fields := strings.Split(tc.value, ",")
			if creds.Bucket != fields[0] || creds.Region != fields[1] || creds.Endpoint != fields[2] || creds.Access != fields[3] || creds.Secret != fields[4] {
				t.Fatalf("fields were not preserved")
			}
		})
	}
}

func TestUploadedURLReservedCharacters(t *testing.T) {
	storageServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("ETag", `"abc"`)
	})
	for _, tenant := range []string{"", "tenant", "dept?archive", "dept#archive", "dept%archive", "dept+archive", "dept&archive", "dept%2Farchive", "dept?archive#100%+&"} {
		t.Run(tenant, func(t *testing.T) {
			body, ct := multipartBody(t, 1, false)
			req := httptest.NewRequest(http.MethodPost, "/dms/upload", bytes.NewReader(body))
			req.Header.Set("Content-Type", ct)
			req.Header.Set("X-Okapi-Tenant", tenant)
			res := httptest.NewRecorder()
			Handler(context.Background()).ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("upload: %d %s", res.Code, res.Body.String())
			}
			var uploaded Uploaded
			if err := json.Unmarshal(res.Body.Bytes(), &uploaded); err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(uploaded.Url)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Path != "/documents/"+uploaded.Key || parsed.RawQuery != "" || parsed.Fragment != "" {
				t.Fatalf("URL changed key semantics: %s", uploaded.Url)
			}
			if !validKey(uploaded.Key, tenant) {
				t.Fatalf("unexpected key %q", uploaded.Key)
			}
			if tenant != "" && !strings.HasPrefix(uploaded.Key, tenant+"/") {
				t.Fatalf("tenant changed: %q", uploaded.Key)
			}
		})
	}
}
