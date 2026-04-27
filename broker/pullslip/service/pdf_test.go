package psservice

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestBarcodeWidth(t *testing.T) {
	tests := []struct {
		name     string
		dataLen  int
		expected int
	}{
		{"zero length uses minimum", 0, 200},
		{"short string uses minimum", 1, 200},
		{"typical request ID", 7, 336},
		{"longer ID scales up", 20, 765},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, barcodeWidth(tc.dataLen))
		})
	}
}

func TestGetBarcodeBase64(t *testing.T) {
	encoded, err := getBarcodeBase64("REQ-123")
	assert.NoError(t, err)
	assert.NotEmpty(t, encoded)

	raw, err := base64.StdEncoding.DecodeString(encoded)
	assert.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(raw))
	assert.NoError(t, err)
	bounds := img.Bounds()
	assert.Equal(t, 336, bounds.Dx())
	assert.Equal(t, 67, bounds.Dy())
}

func TestRenderPullSlipHTML(t *testing.T) {
	html, err := renderPullSlipHTML(PullSlipData{
		ReqId:          "REQ-123",
		PickupLocation: "Main Library",
		Title:          "Big Shark",
		Author:         "John Doe",
		DueDate:        "2026-01-01",
		ReturnAddress:  "1 Test Street",
		BarcodeBase64:  "abc123",
	})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(html, "REQ-123"))
	assert.True(t, strings.Contains(html, "Main Library"))
	assert.True(t, strings.Contains(html, "data:image/png;base64,abc123"))
}

func TestRenderPullSlipHTML_InvalidTemplate(t *testing.T) {
	// Temporarily swap pullSlipTemplate with an invalid one
	orig := pullSlipTemplate
	defer func() { pullSlipTemplate = orig }()
	pullSlipTemplate = `{{.Unclosed`

	_, err := renderPullSlipHTML(PullSlipData{ReqId: "X"})
	assert.Error(t, err)
}

func TestRenderPullSlipHTML_ExecuteError(t *testing.T) {
	// A template that calls a function on a field that panics/errors at execute time
	orig := pullSlipTemplate
	defer func() { pullSlipTemplate = orig }()
	// Use a template that references a non-existent function to trigger execute error
	// The only reliable way in Go templates: call.option "missingkey=error" with unknown key on a map
	pullSlipTemplate = `{{index . "nonexistent"}}`

	_, err := renderPullSlipHTML(PullSlipData{ReqId: "X"})
	// Execute on a struct with map-access fails
	assert.Error(t, err)
}

func TestGeneratePdfPullSlip_Defaults(t *testing.T) {
	svc := &PdfService{}
	pr := pr_db.PatronRequest{
		RequesterReqID: pgtype.Text{
			String: "REQ-DEFAULTS",
			Valid:  true,
		},
		// No bibliographic info — all fields should fall back to DEFAULT_FOR_NO_VALUE
	}
	pdfBytes, err := svc.GeneratePdfPullSlip(pr)
	assert.NoError(t, err)
	assert.NotEmpty(t, pdfBytes)
	// PDF magic bytes: %PDF
	assert.Equal(t, "%PDF", string(pdfBytes[:4]))
}

func TestGeneratePdfPullSlip_WithBibliographicInfo(t *testing.T) {
	svc := &PdfService{}
	pr := pr_db.PatronRequest{
		ID: "REQ-BIB",
		RequesterReqID: pgtype.Text{
			String: "REQ-BIB",
			Valid:  true,
		},
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{
				Title:  "Great White Shark",
				Author: "Jane Doe",
			},
		},
	}
	pdfBytes, err := svc.GeneratePdfPullSlip(pr)
	assert.NoError(t, err)
	assert.NotEmpty(t, pdfBytes)
	assert.Equal(t, "%PDF", string(pdfBytes[:4]))
}

func TestGetBarcodeBase64_EncodeError(t *testing.T) {
	// Characters > 127 are outside code128 charset, causing encode to fail
	_, err := getBarcodeBase64("\x80invalid")
	assert.Error(t, err)
}

func TestGeneratePdfPullSlip_BarcodeError(t *testing.T) {
	svc := &PdfService{}
	pr := pr_db.PatronRequest{
		ID: "\x80invalid", // non-ASCII causes code128 encoding to fail
	}
	_, err := svc.GeneratePdfPullSlip(pr)
	assert.Error(t, err)
}

func TestGeneratePdfPullSlip_TemplateError(t *testing.T) {
	orig := pullSlipTemplate
	defer func() { pullSlipTemplate = orig }()
	pullSlipTemplate = `{{.Unclosed`

	svc := &PdfService{}
	pr := pr_db.PatronRequest{ID: "REQ-X"}
	_, err := svc.GeneratePdfPullSlip(pr)
	assert.Error(t, err)
}

func TestGeneratePdfPullSlip_TempFileError(t *testing.T) {
	// Point TMPDIR to a non-existent directory so os.CreateTemp fails
	t.Setenv("TMPDIR", "/nonexistent-dir-for-test")
	t.Setenv("TMP", "/nonexistent-dir-for-test")
	t.Setenv("TEMP", "/nonexistent-dir-for-test")

	svc := &PdfService{}
	pr := pr_db.PatronRequest{ID: "REQ-TMPFAIL"}
	_, err := svc.GeneratePdfPullSlip(pr)
	assert.Error(t, err)
}
