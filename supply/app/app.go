package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var Host = cmp.Or(os.Getenv("HOST"), "")
var Port = cmp.Or(os.Getenv("HTTP_PORT"), "8086")

type BucketCreds struct {
	Bucket   string
	Region   string
	Endpoint string
	Access   string
	Secret   string
	Insecure bool
}

// Start serves Supply until ctx is canceled, then drains requests for up to 20 seconds.
func Start(ctx context.Context) error {
	logger, err := Logger()
	if err != nil {
		return fmt.Errorf("configure logger: %w", err)
	}
	slog.SetDefault(logger)
	handler, err := configuredHandler()
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, Addr: fmt.Sprintf("%s:%s", Host, Port), ReadHeaderTimeout: 10 * time.Second}
	slog.InfoContext(ctx, "Starting Supply", "address", server.Addr)
	return serve(ctx, server, 20*time.Second)
}

// Signal cancellation starts draining; request cancellation happens only after draining fails.
func serve(ctx context.Context, server *http.Server, timeout time.Duration) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	return serveListener(ctx, server, listener, timeout)
}

func serveListener(ctx context.Context, server *http.Server, listener net.Listener, timeout time.Duration) error {
	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	server.BaseContext = func(net.Listener) context.Context { return requestCtx }
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		if err != nil {
			cancelRequests()
			closeErr := server.Close()
			<-done
			return fmt.Errorf("shutdown HTTP: %w", errors.Join(err, closeErr))
		}
		serveErr := <-done
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", serveErr)
		}
		return nil
	}
}

func uploadLimit() (int64, error) {
	value, set := os.LookupEnv("MOD_DMS_MAX_UPLOAD_BYTES")
	if !set {
		return 100 * 1024 * 1024, nil
	}
	limit, err := strconv.ParseInt(value, 10, 64)
	if err != nil || limit <= 0 {
		return 0, errors.New("MOD_DMS_MAX_UPLOAD_BYTES must be a positive decimal integer")
	}
	return limit, nil
}

func validTenant(tenant string) bool {
	return tenant != "." && tenant != ".." && !strings.ContainsAny(tenant, "/\\") && !strings.ContainsFunc(tenant, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func validKey(key, tenant string) bool {
	if !validTenant(tenant) {
		return false
	}
	parts := strings.Split(key, "/")
	if tenant == "" {
		return len(parts) == 1 && uuid.Validate(parts[0]) == nil
	}
	return len(parts) == 2 && parts[0] == tenant && uuid.Validate(parts[1]) == nil
}

func InitLogger() {
	logger, err := Logger()
	if err != nil {
		log.Fatalf("Logger configuration failed: %s", err)
	}
	slog.SetDefault(logger)
}

func Logger() (*slog.Logger, error) {
	var logger *slog.Logger
	logLevel := &slog.LevelVar{}
	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	if os.Getenv("LOG_JSON") == "true" {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stdout, opts))
	}

	desiredLevel, levelSet := os.LookupEnv("LOG_LEVEL")
	if levelSet {
		var newLevel slog.Level
		if err := newLevel.UnmarshalText([]byte(desiredLevel)); err != nil {
			return nil, fmt.Errorf("unsupported log level '%s'", desiredLevel)
		}
		logLevel.Set(newLevel)
	}

	return logger, nil
}

// Handler builds the HTTP routes with a shared storage client and the current configuration.
// Invalid configuration produces a handler that returns HTTP 503, including for health checks.
func Handler(ctx context.Context) http.Handler {
	handler, err := configuredHandler()
	if err != nil {
		slog.ErrorContext(ctx, "Invalid Supply configuration", "error", err)
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		})
	}
	return handler
}

func configuredHandler() (http.Handler, error) {
	bucket, client, err := getBucketClient()
	if err != nil {
		return nil, fmt.Errorf("configure bucket: %w", err)
	}
	limit, err := uploadLimit()
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /dms/upload", func(w http.ResponseWriter, req *http.Request) {
		handleUpload(w, req, limit, bucket, client)
	})
	mux.HandleFunc("DELETE /dms/upload/{key...}", func(w http.ResponseWriter, req *http.Request) {
		handleDelete(w, req, bucket, client)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Reject malformed keys before ServeMux can redirect a cleaned path.
		if req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/dms/upload/") && !validKey(strings.TrimPrefix(req.URL.Path, "/dms/upload/"), req.Header.Get("X-Okapi-Tenant")) {
			http.Error(w, "Invalid key, can only delete UUIDs with optional matching tenant prefix", http.StatusBadRequest)
			return
		}
		mux.ServeHTTP(w, req)
	}), nil
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("OK"))
}

func Creds() (BucketCreds, error) {
	splitEnv := strings.Split(os.Getenv("MOD_DMS_BUCKET"), ",")
	if len(splitEnv) != 5 {
		return BucketCreds{}, errors.New("MOD_DMS_BUCKET must contain exactly five comma-delimited components")
	}

	return BucketCreds{
		Bucket:   splitEnv[0],
		Region:   splitEnv[1],
		Endpoint: splitEnv[2],
		Access:   splitEnv[3],
		Secret:   splitEnv[4],
		Insecure: os.Getenv("MOD_DMS_INSECURE") == "true",
	}, nil
}

func getBucketClient() (string, *minio.Client, error) {
	b, err := Creds()
	if err != nil {
		return "", nil, err
	}

	minioClient, err := minio.New(b.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(b.Access, b.Secret, ""),
		Region: b.Region,
		Secure: !b.Insecure,
	})

	return b.Bucket, minioClient, err
}

type Uploaded struct {
	Url string `json:"url"`
	Key string `json:"key"`
}

func handleUpload(w http.ResponseWriter, req *http.Request, limit int64, bucket string, minioClient *minio.Client) {
	tenant := req.Header.Get("X-Okapi-Tenant")
	if !validTenant(tenant) {
		http.Error(w, "Invalid tenant", http.StatusBadRequest)
		return
	}
	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		slog.WarnContext(req.Context(), "Unexpected content type", "contentType", req.Header.Get("Content-Type"))
		http.Error(w, "Expected multipart/form-data", http.StatusUnsupportedMediaType)
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, limit)
	defer func() {
		if req.MultipartForm != nil {
			if err := req.MultipartForm.RemoveAll(); err != nil {
				slog.WarnContext(req.Context(), "Remove multipart temporary files", "error", err)
			}
		}
	}()
	err = req.ParseMultipartForm(32 << 20)
	if err == nil {
		// Include any multipart epilogue in the total request-size limit.
		_, err = io.Copy(io.Discard, req.Body)
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Upload request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Error receiving file", http.StatusBadRequest)
		}
		return
	}
	file, fileHeader, err := req.FormFile("file")
	if err != nil {
		slog.WarnContext(req.Context(), "Error retrieving the file from multipart", "error", err)
		http.Error(w, "Error receiving file", http.StatusBadRequest)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.WarnContext(req.Context(), "Error closing uploaded file", "error", err)
		}
	}()

	slog.DebugContext(req.Context(), "Received file")

	fileType := fileHeader.Header.Get("Content-Type")

	validTypeString, hasValidTypes := os.LookupEnv("MOD_DMS_TYPES")
	validTypes := strings.Split(validTypeString, ",")
	if hasValidTypes && !slices.Contains(validTypes, fileType) {
		h := w.Header()

		// http.Error does this so we will too
		h.Del("Content-Length")
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")

		w.WriteHeader(http.StatusUnsupportedMediaType)
		_, _ = fmt.Fprintln(w, "Unsupported file type")
		// h.Set("Accept-Post", strings.Join(validTypes, ", "))
		return
	}

	slog.DebugContext(req.Context(), "Got client for bucket", "bucket", bucket)

	filename := uuid.NewString()
	if tenant != "" {
		// Tenant identity must be supplied through a trusted Okapi route.
		filename = tenant + "/" + filename
	}

	_, err = minioClient.PutObject(req.Context(), bucket, filename, file, fileHeader.Size, minio.PutObjectOptions{
		ContentType: fileType,
	})
	if err != nil {
		slog.ErrorContext(req.Context(), "Error sending to bucket", "error", err, "bucket", bucket)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	slog.DebugContext(req.Context(), "Uploaded file")

	objectURL := *minioClient.EndpointURL()
	objectURL.Path = "/" + bucket + "/" + filename
	objectURL.RawPath = ""
	response := Uploaded{
		Url: objectURL.String(),
		Key: filename,
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.ErrorContext(req.Context(), "Error encoding response", "error", err)
	}
}

func handleDelete(w http.ResponseWriter, req *http.Request, bucket string, minioClient *minio.Client) {
	key := req.PathValue("key")
	tenant := req.Header.Get("X-Okapi-Tenant")

	if !validKey(key, tenant) {
		slog.WarnContext(req.Context(), "Attempt to delete object with unexpected key")
		http.Error(w, "Invalid key, can only delete UUIDs with optional tenant prefix", http.StatusBadRequest)
		return
	}

	// NB this does not error when the object does not exist, just silently is fine with it,
	// perhaps to prevent using it for discovery?
	err := minioClient.RemoveObject(req.Context(), bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		slog.ErrorContext(req.Context(), "Error removing object", "error", err, "bucket", bucket)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
}
