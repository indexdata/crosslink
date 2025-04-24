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
	"net/textproto"
	"os"
	"strings"
	"testing"

	"github.com/indexdata/mod-dms/app"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	minioContainer "github.com/testcontainers/testcontainers-go/modules/minio"
)

func TestMain(m *testing.M) {
	app.InitLogger()
	ctx := context.Background()

	con, err := minioContainer.Run(ctx, "minio/minio:RELEASE.2025-03-12T18-04-18Z")
	if err != nil {
		panic(fmt.Sprintf("failed to start minio: %s", err))
	}
	defer func() { _ = con.Terminate(ctx) }()

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

	err = minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region})
	if err != nil {
		log.Fatalln(err)
	}

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

func uploadFile(t *testing.T, contents string, tenant string, contentType string) *httptest.ResponseRecorder {
	tempFile, err := os.CreateTemp("", "dms-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())
	_, err = tempFile.Write([]byte(contents))
	if err != nil {
		t.Fatal(err)
	}
	tempFile.Close()

	// buffer to hold multipart
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	h := make(textproto.MIMEHeader)
	// mod-dms expects the uploaded file in the "file" field
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename="%s"`, tempFile.Name()))
	h.Set("Content-Type", contentType)
	formFile, err := writer.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}

	fileData, err := os.ReadFile(tempFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	_, err = formFile.Write(fileData)
	if err != nil {
		t.Fatal(err)
	}
	writer.Close()

	req := httptest.NewRequest("POST", "/dms/upload", &b)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Okapi-Tenant", tenant)
	res := httptest.NewRecorder()

	handler := app.Handler(context.Background())
	handler.ServeHTTP(res, req)

	return res
}

func uploadFileAndParse(t *testing.T, contents string, tenant string, contentType string) app.Uploaded {
	res := uploadFile(t, contents, tenant, contentType)

	if status := res.Code; status != http.StatusOK {
		t.Errorf("Upload handler returned non-OK status: %v", status)
	}

	var data app.Uploaded
	err := json.Unmarshal(res.Body.Bytes(), &data)
	if err != nil {
		t.Errorf("Error parsing response from POST to upload endpoint: %s", err)
	}

	return data
}

func TestUpload(t *testing.T) {
	expected := "String for testing"
	tenant := "sometenant"
	contentType := "image/png"
	uploadResponse := uploadFileAndParse(t, expected, tenant, contentType)

	res, err := http.Get(uploadResponse.Url)
	if err != nil {
		t.Errorf("Error attempting to request returned link: %s", err)
	}

	if !strings.HasPrefix(uploadResponse.Key, tenant+"/") {
		t.Errorf("Key not prefixed with tenant: %s", uploadResponse.Key)
	}

	if status := res.StatusCode; status != http.StatusOK {
		t.Errorf("Accessing returned link returned non-OK status: %v", status)
	}

	if ct := res.Header.Get("Content-Type"); ct != contentType {
		t.Errorf("Accessing returned link returned unexpected content type: %v", ct)
	}

	rb, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(rb) != expected {
		t.Errorf("Accessing returned link returned unexpected body: %v", rb)
	}
}

func TestDelete(t *testing.T) {
	content := "String for testing"
	uploadResponse := uploadFileAndParse(t, content, "", "application/pdf")

	req := httptest.NewRequest("DELETE", "/dms/upload/"+uploadResponse.Key, nil)
	w := httptest.NewRecorder()

	handler := app.Handler(context.Background())
	handler.ServeHTTP(w, req)

	if status := w.Code; status != http.StatusOK {
		t.Errorf("Delete handler returned non-OK status: %v", status)
	}

	res, err := http.Get(uploadResponse.Url)
	if err != nil {
		t.Errorf("Error attempting to request returned link: %s\n", err)
	}

	if status := res.StatusCode; status != http.StatusNotFound {
		t.Errorf("Accessing link after delete returned non-404 status: %v", status)
	}
}

func TestDeleteErr(t *testing.T) {
	handler := app.Handler(context.Background())

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/dms/upload/nonsense", nil)
	handler.ServeHTTP(w, req)

	if status := w.Code; status != http.StatusBadRequest {
		t.Errorf("Delete handler failed to return 400 on attempt to delete non-uuid named file: %v", status)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/dms/upload/d8290e68-bfbb-3bc8-b621-5a9590aa29fd", nil)
	handler.ServeHTTP(w, req)

	if status := w.Code; status != http.StatusOK {
		t.Errorf("Delete handler returned non-OK status attempting to delete validly named (but non-existing) object: %v", status)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/dms/upload/different/d8290e68-bfbb-3bc8-b621-5a9590aa29fd", nil)
	req.Header.Set("X-Okapi-Tenant", "sometenant")
	handler.ServeHTTP(w, req)

	if status := w.Code; status != http.StatusBadRequest {
		t.Errorf("Delete handler failed to return 400 on attempt to delete file not matching tenant: %v", status)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/dms/upload/sometenant/d8290e68-bfbb-3bc8-b621-5a9590aa29fd", nil)
	handler.ServeHTTP(w, req)

	if status := w.Code; status != http.StatusOK {
		t.Errorf("Delete handler returned non-OK status attempting to delete (with tenant) validly named (but non-existing) object: %v", status)
	}
}

func TestRestrictContentType(t *testing.T) {
	content := "String for testing"
	validTypeString := "image/png,application/pdf"
	os.Setenv("MOD_DMS_TYPES", validTypeString)

	res := uploadFile(t, content, "", "application/pdf")
	if status := res.Code; status != http.StatusOK {
		t.Errorf("Upload handler returned non-OK status for content type expected to be accepted: %v", status)
	}

	res = uploadFile(t, content, "", "application/javascript")
	if status := res.Code; status != http.StatusUnsupportedMediaType {
		t.Errorf("Upload handler returned non-415 status for unsupported file part content type: %v", status)
	}

	handler := app.Handler(context.Background())
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/dms/upload", nil)
	req.Header.Set("Content-Type", "not/right")
	handler.ServeHTTP(w, req)
	if status := w.Code; status != http.StatusUnsupportedMediaType {
		t.Errorf("Upload handler returned non-415 status for invalid request content type: %v", status)
	}
}
