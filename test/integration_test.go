package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/indexdata/mod-dms/app"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	minioContainer "github.com/testcontainers/testcontainers-go/modules/minio"
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	con, err := minioContainer.Run(ctx, "minio/minio:RELEASE.2025-03-12T18-04-18Z")
	if err != nil {
		panic(fmt.Sprintf("failed to start minio: %s", err))
	}
	defer con.Terminate(ctx)

	conStr, err := con.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to get minio connection string: %s", err))
	}

	bucket := "dmstest"
	region := "us-east-1"
	access := "minioadmin"
	secret := "minioadmin"

	minioClient, err := minio.New(conStr, &minio.Options{
		Creds:  credentials.NewStaticV4(access, secret, ""),
		Region: region,
		Secure: false,
	})
	if err != nil {
		log.Fatalln(err)
	}

	minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region})

	// configure the bucket as sufficiently public that we can follow the URL we get back
	policy := fmt.Sprintf(`{"Version": "2012-10-17","Statement": [{"Action": ["s3:GetObject"],"Effect": "Allow","Principal": {"AWS": ["*"]},"Resource": ["arn:aws:s3:::%s/*"],"Sid": ""}]}`, bucket)
	err = minioClient.SetBucketPolicy(context.Background(), bucket, policy)
	if err != nil {
		log.Fatalln(err)
	}

	os.Setenv("MOD_DMS_BUCKET", fmt.Sprintf("%s,%s,%s,%s,%s", bucket, region, conStr, access, secret))
	os.Setenv("MOD_DMS_INSECURE", "true")

	code := m.Run()
	os.Exit(code)
}
func TestUpload(t *testing.T) {
	// it'd be nice to avoid a temporary file but CreateFormFile is convenient
	expected := "String for testing"
	tempFile, err := os.CreateTemp("", "dms-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Write([]byte(expected))
	tempFile.Close()

	// buffer to hold multipart
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	// mod-dms expects the uploaded file in the "file" field
	formFile, err := writer.CreateFormFile("file", tempFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	fileData, err := os.ReadFile(tempFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	formFile.Write(fileData)
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", &b)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	handler := app.Handler(context.Background())
	handler.ServeHTTP(w, req)

	if status := w.Code; status != http.StatusOK {
		t.Errorf("Upload handler returned non-OK status: %v", status)
	}

	var data map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &data)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(data["url"])
	res, err := http.Get((data["url"].(string)))
	if err != nil {
		fmt.Printf("error making http request: %s\n", err)
		os.Exit(1)
	}

	if status := res.StatusCode; status != http.StatusOK {
		t.Errorf("Accessing returned link returned non-OK status: %v", status)
	}

	rb, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(rb) != expected {
		t.Errorf("Accessing returned link returned unexpected body: %v", rb)
	}
}
