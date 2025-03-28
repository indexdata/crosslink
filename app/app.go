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
	mux.HandleFunc("DELETE /upload/{id}", handleDelete)
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

type Uploaded struct {
	Url string `json:"url"`
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

	b, err := GetCreds()
	if err != nil {
		fmt.Fprintf(w, "Error obtaining S3 bucket: %v", err)
		http.Error(w, "Error obtaining S3 bucket", http.StatusBadRequest)
		return
	}

	log.Printf("Got credentials")

	// Can we pool these?
	minioClient, err := minio.New(b.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(b.Access, b.Secret, ""),
		Region: b.Region,
		Secure: !b.Insecure,
	})
	if err != nil {
		log.Fatalln(err)
	}

	// TODO add create a prefix based on tenant header
	filename := uuid.NewString()

	// TODO http.DetectContentType
	_, err = minioClient.PutObject(context.Background(), b.Bucket, filename, file, fileHeader.Size, minio.PutObjectOptions{
		ContentType: "text/html",
	})
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("Uploaded file")

	log.Println(minioClient.EndpointURL())
	response := Uploaded{Url: minioClient.EndpointURL().String() + "/" + b.Bucket + "/" + filename}
	json.NewEncoder(w).Encode(response)
}

func handleDelete(w http.ResponseWriter, req *http.Request) {
	if req.Method != "DELETE" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
}
