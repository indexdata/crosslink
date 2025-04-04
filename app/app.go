package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var Host = cmp.Or(os.Getenv("HOST"), "localhost")
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
	handler := Handler(ctx)
	addr := fmt.Sprintf("%s:%s", Host, Port)
	log.Printf("Starting mod-dms at %s...", addr)
	s := &http.Server{
		Handler: handler,
		Addr:    addr,
	}

	log.Fatal(s.ListenAndServe())
}

func Handler(ctx context.Context) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", handleUpload)
	mux.HandleFunc("DELETE /upload/{key}", handleDelete)
	return mux
}

func GetCreds() (BucketCreds, error) {
	splitEnv := strings.Split(os.Getenv("MOD_DMS_BUCKET"), ",")
	if len(splitEnv) < 5 {
		return BucketCreds{}, errors.New("environment not configured")
	}

	_, insecure := os.LookupEnv("MOD_DMS_INSECURE")

	return BucketCreds{
		Bucket:   splitEnv[0],
		Region:   splitEnv[1],
		Endpoint: splitEnv[2],
		Access:   splitEnv[3],
		Secret:   splitEnv[4],
		Insecure: insecure,
	}, nil
}

func getBucketClient() (string, *minio.Client, error) {
	b, err := GetCreds()
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
	if req.Method != "POST" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Ultimately better to stream via MultipartReader...
	file, fileHeader, err := req.FormFile("file")
	if err != nil {
		fmt.Fprintf(w, "Error retrieving the file: %v", err)
		http.Error(w, "Error receiving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	log.Printf("Received file")

	fileType := fileHeader.Header.Get("Content-Type")

	validTypeString, hasValidTypes := os.LookupEnv("MOD_DMS_TYPES")
	validTypes := strings.Split(validTypeString, ",")
	if hasValidTypes && !slices.Contains(validTypes, fileType) {
		h := w.Header()

		// http.Error does this so we will too
		h.Del("Content-Length")
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")

		// Politely inform the client what we accept
		h.Set("Accept-Post", strings.Join(validTypes, ", "))

		w.WriteHeader(http.StatusUnsupportedMediaType)
		fmt.Fprintln(w, "Unsupported file type")
		return
	}

	bucket, minioClient, err := getBucketClient()
	if err != nil {
		fmt.Fprintf(w, "Error obtaining client for S3 bucket: %v", err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	log.Printf("Got client for bucket %s", bucket)

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
		log.Fatalln(err)
	}

	log.Printf("Uploaded file")

	log.Println(minioClient.EndpointURL())
	response := Uploaded{
		Url: minioClient.EndpointURL().String() + "/" + bucket + "/" + filename,
		Key: filename,
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Fatalln(err)
	}
}

func handleDelete(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	err := uuid.Validate(key)
	if err != nil {
		http.Error(w, "Invalid key, can only delete UUIDs with optional prefix", http.StatusBadRequest)
		return
	}

	bucket, minioClient, err := getBucketClient()
	if err != nil {
		fmt.Fprintf(w, "Error obtaining client for S3 bucket: %v", err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	err = minioClient.RemoveObject(context.Background(), bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		log.Fatalln(err)
	}
}
