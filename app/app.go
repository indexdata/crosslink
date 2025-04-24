package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var Host = cmp.Or(os.Getenv("HOST"), "")
var Port = cmp.Or(os.Getenv("PORT"), "8086")

type BucketCreds struct {
	Bucket   string
	Region   string
	Endpoint string
	Access   string
	Secret   string
	Insecure bool
}

func Start(ctx context.Context) {
	InitLogger()

	_, err := Creds()
	if err != nil {
		log.Fatalf("Bucket configuration failed: %s", err)
	}

	handler := Handler(ctx)
	addr := fmt.Sprintf("%s:%s", Host, Port)
	log.Printf("Starting mod-dms at %s...", addr)
	s := &http.Server{
		Handler: handler,
		Addr:    addr,
	}

	log.Fatal(s.ListenAndServe())
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

func Handler(ctx context.Context) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /dms/upload", handleUpload)
	mux.HandleFunc("DELETE /dms/upload/{key...}", handleDelete)
	return mux
}

func Creds() (BucketCreds, error) {
	splitEnv := strings.Split(os.Getenv("MOD_DMS_BUCKET"), ",")
	if len(splitEnv) < 5 {
		return BucketCreds{}, errors.New("environment not configured")
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

	// Can we pool these?
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

func handleUpload(w http.ResponseWriter, req *http.Request) {
	if contentType := req.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "multipart/form-data") {
		slog.Warn("Unexpected content type", "contentType", contentType)
		http.Error(w, "Expected multipart/form-data", http.StatusUnsupportedMediaType)
		return
	}

	// Ultimately better to stream via MultipartReader...
	file, fileHeader, err := req.FormFile("file")
	if err != nil {
		slog.Warn("Error retrieving the file from multipart", "error", slog.Any("error", err))
		http.Error(w, "Error receiving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	slog.Debug("Received file")

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
		fmt.Fprintln(w, "Unsupported file type")
		// h.Set("Accept-Post", strings.Join(validTypes, ", "))
		return
	}

	bucket, minioClient, err := getBucketClient()
	if err != nil {
		slog.Error("Error obtaining client for S3 bucket", "error", slog.Any("error", err), "bucket", bucket)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	slog.Debug("Got client for bucket", "bucket", bucket)

	filename := uuid.NewString()
	tenant := req.Header.Get("X-Okapi-Tenant")
	if tenant != "" {
		// we're behind Okapi, this is not sanitised for arbitrary header values
		filename = tenant + "/" + filename
	}

	_, err = minioClient.PutObject(context.Background(), bucket, filename, file, fileHeader.Size, minio.PutObjectOptions{
		ContentType: fileType,
	})
	if err != nil {
		slog.Error("Error sending to bucket", "error", slog.Any("error", err), "bucket", bucket, "key", filename)
	}

	slog.Debug("Uploaded file")

	log.Println(minioClient.EndpointURL())
	response := Uploaded{
		Url: minioClient.EndpointURL().String() + "/" + bucket + "/" + filename,
		Key: filename,
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Error encoding response", "error", slog.Any("error", err))
	}
}

func handleDelete(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	tenant := req.Header.Get("X-Okapi-Tenant")

	k := strings.Split(key, "/")
	if uuid.Validate(k[len(k)-1]) != nil || (tenant != "" && tenant != k[0]) {
		slog.Warn("Attempt to delete object with unexpected key", "key", key)
		http.Error(w, "Invalid key, can only delete UUIDs with optional tenant prefix", http.StatusBadRequest)
		return
	}

	bucket, minioClient, err := getBucketClient()
	if err != nil {
		slog.Error("Error obtaining client for S3 bucket", "error", slog.Any("error", err), "bucket", bucket)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	// NB this does not error when the object does not exist, just silently is fine with it,
	// perhaps to prevent using it for discovery?
	err = minioClient.RemoveObject(context.Background(), bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		slog.Error("Error removing objecct", "error", slog.Any("error", err), "bucket", bucket, "key", key)
	}
}
