package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/supply/app"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	minioContainer "github.com/testcontainers/testcontainers-go/modules/minio"
)

func TestMain(m *testing.M) { os.Exit(runTests(m)) }

func runTests(m *testing.M) (code int) {
	app.InitLogger()
	ctx := context.Background()

	con, err := minioContainer.Run(ctx, "", testcontainers.CustomizeRequest(testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:        "minio",
				BuildLogWriter: os.Stderr,
			},
		},
	}))
	if err != nil {
		slog.Error("Start MinIO", "error", err)
		return 1
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := con.Terminate(cleanupCtx); err != nil {
			slog.Error("Terminate MinIO", "error", err)
			if code == 0 {
				code = 1
			}
		}
	}()

	conStr, err := con.ConnectionString(ctx)
	if err != nil {
		slog.Error("Get MinIO connection string", "error", err)
		return 1
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
		slog.Error("MinIO setup failed", "error", err)
		return 1
	}

	err = minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region})
	if err != nil {
		slog.Error("MinIO setup failed", "error", err)
		return 1
	}

	// configure the bucket as sufficiently public that we can follow the URL we get back
	policy := fmt.Sprintf(`{"Version": "2012-10-17","Statement": [{"Action": ["s3:GetObject"],"Effect": "Allow","Principal": {"AWS": ["*"]},"Resource": ["arn:aws:s3:::%s/*"],"Sid": ""}]}`, bucket)
	err = minioClient.SetBucketPolicy(context.Background(), bucket, policy)
	if err != nil {
		slog.Error("MinIO setup failed", "error", err)
		return 1
	}

	if err := os.Setenv("MOD_DMS_BUCKET", fmt.Sprintf("%s,%s,%s,%s,%s", bucket, region, conStr, access, secret)); err != nil {
		slog.Error("MinIO setup failed", "error", err)
		return 1
	}
	if err := os.Setenv("MOD_DMS_INSECURE", "true"); err != nil {
		slog.Error("MinIO setup failed", "error", err)
		return 1
	}

	return m.Run()
}

func uploadFile(t *testing.T, contents string, tenant string, contentType string) *httptest.ResponseRecorder {
	tempFile, err := os.CreateTemp("", "dms-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(tempFile.Name()); err != nil {
			t.Error(err)
		}
	}()
	_, err = tempFile.Write([]byte(contents))
	if err != nil {
		t.Fatal(err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatal(err)
	}

	// buffer to hold multipart
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	h := make(textproto.MIMEHeader)
	// Supply expects the uploaded file in the "file" field
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
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

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
		t.Fatalf("Upload handler returned non-OK status: %v", status)
	}

	var data app.Uploaded
	err := json.Unmarshal(res.Body.Bytes(), &data)
	if err != nil {
		t.Fatalf("Error parsing response from POST to upload endpoint: %s", err)
	}

	return data
}

func TestUpload(t *testing.T) {
	expected := "String for testing"
	tenant := "sometenant"
	contentType := "image/png"
	uploadResponse := uploadFileAndParse(t, expected, tenant, contentType)

	res, err := (&http.Client{Timeout: 10 * time.Second}).Get(uploadResponse.Url)
	if err != nil {
		t.Fatalf("Error attempting to request returned link: %s", err)
	}

	if !strings.HasPrefix(uploadResponse.Key, tenant+"/") {
		t.Errorf("Key not prefixed with tenant: %s", uploadResponse.Key)
	}

	defer func() { _ = res.Body.Close() }()
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

	res, err := (&http.Client{Timeout: 10 * time.Second}).Get(uploadResponse.Url)
	if err != nil {
		t.Fatalf("Error attempting to request returned link: %s\n", err)
	}

	defer func() { _ = res.Body.Close() }()
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

	if status := w.Code; status != http.StatusBadRequest {
		t.Errorf("Delete handler should reject a tenant key without a tenant header: %v", status)
	}
}

func TestRestrictContentType(t *testing.T) {
	content := "String for testing"
	validTypeString := "image/png,application/pdf"
	t.Setenv("MOD_DMS_TYPES", validTypeString)

	res := uploadFile(t, content, "", "application/pdf")
	if status := res.Code; status != http.StatusOK {
		t.Fatalf("Upload handler returned non-OK status for content type expected to be accepted: %v", status)
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

func TestTenantDelete(t *testing.T) {
	uploaded := uploadFileAndParse(t, "tenant document", "sometenant", "application/pdf")
	handler := app.Handler(context.Background())
	for _, tenant := range []string{"", "other", "sometenant", "sometenant"} {
		req := httptest.NewRequest(http.MethodDelete, "/dms/upload/"+uploaded.Key, nil)
		req.Header.Set("X-Okapi-Tenant", tenant)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		want := http.StatusBadRequest
		if tenant == "sometenant" {
			want = http.StatusOK
		}
		if res.Code != want {
			t.Fatalf("tenant %q: status %d, want %d", tenant, res.Code, want)
		}
		if tenant != "sometenant" {
			download, err := (&http.Client{Timeout: 10 * time.Second}).Get(uploaded.Url)
			if err != nil {
				t.Fatal(err)
			}
			_ = download.Body.Close()
			if download.StatusCode != http.StatusOK {
				t.Fatal("rejected deletion removed the object")
			}
		}
	}
}
