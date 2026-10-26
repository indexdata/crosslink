package psservice

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/carlos7ags/folio/reader"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/email"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var appCtx = common.CreateExtCtxWithArgs(context.Background(), nil)

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

func TestGeneratePdfPullSlip_Defaults(t *testing.T) {
	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{
		RequesterReqID: pgtype.Text{
			String: "REQ-DEFAULTS",
			Valid:  true,
		},
		// No bibliographic info — all fields should fall back to DEFAULT_FOR_NO_VALUE
	}
	pdfBytes, err := svc.GeneratePdfPullSlip(appCtx, pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.NoError(t, err)
	assert.NotEmpty(t, pdfBytes)
	// PDF magic bytes: %PDF
	assert.Equal(t, "%PDF", string(pdfBytes[:4]))
}

func TestGeneratePdfPullSlip_WithBibliographicInfo(t *testing.T) {
	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
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
	pdfBytes, err := svc.GeneratePdfPullSlip(appCtx, pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.NoError(t, err)
	assert.NotEmpty(t, pdfBytes)
	assert.Equal(t, "%PDF", string(pdfBytes[:4]))
}

func TestGeneratePdfPullSlip_FullData(t *testing.T) {
	callNumber := "QA76.9.A25"
	dueDate := utils.XSDDateTime{Time: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)}

	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{
		ID: "REQ-FULL",
		RequesterReqID: pgtype.Text{
			String: "REQ-FULL",
			Valid:  true,
		},
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{
				Title:                  "The Art of Computer Programming",
				Author:                 "Donald Knuth",
				Volume:                 "1",
				Issue:                  "2",
				EstimatedNoPages:       "650",
				SupplierUniqueRecordId: "SYS-ID-42",
			},
			PublicationInfo: &iso18626.PublicationInfo{
				Publisher: "Addison-Wesley",
			},
			ServiceInfo: &iso18626.ServiceInfo{
				ServiceType: iso18626.TypeServiceTypeLoan,
				ServiceLevel: &iso18626.TypeSchemeValuePair{
					Text: "EXPRESS",
				},
			},
			RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{
				{
					Address: &iso18626.Address{
						PhysicalAddress: &iso18626.PhysicalAddress{
							Line1: "Pick up at front desk",
						},
					},
				},
			},
		},
		IllResponse: iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				DueDate: &dueDate,
			},
			ReturnInfo: &iso18626.ReturnInfo{
				PhysicalAddress: &iso18626.PhysicalAddress{
					Line1:      "123 Library Lane",
					Line2:      "Suite 4",
					Locality:   "Springfield",
					PostalCode: "12345",
					Region:     &iso18626.TypeSchemeValuePair{Text: "IL"},
					Country:    &iso18626.TypeSchemeValuePair{Text: "US"},
				},
			},
		},
		Items: []pr_db.PrItem{
			{ID: "item-1", CallNumber: &callNumber},
		},
	}

	notes := []pr_db.Notification{
		{Note: pgtype.Text{String: "Handle with care", Valid: true}},
		{Note: pgtype.Text{String: "Rush request", Valid: true}},
	}
	conditions := []pr_db.Notification{
		{Condition: pgtype.Text{String: "No photocopying", Valid: true}},
	}

	pdfBytes, err := svc.GeneratePdfPullSlip(appCtx, pr, notes, conditions)
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
	svc := newSvcWithMock(&mockPrRepo{})
	pr := pr_db.PatronRequest{
		RequesterReqID: pgtype.Text{
			String: "\x80invalid", // non-ASCII causes code128 encoding to fail
			Valid:  true,
		},
	}
	_, err := svc.GeneratePdfPullSlip(appCtx, pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.ErrorContains(t, err, "barcode:")
}

func TestGeneratePdfPullSlip_TemplateError(t *testing.T) {
	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{
		ID: "REQ-X",
		RequesterReqID: pgtype.Text{
			String: "REQ-X",
			Valid:  true,
		},
		RequesterSymbol: pgtype.Text{String: "invalid", Valid: true},
	}
	_, err := svc.GeneratePdfPullSlip(appCtx, pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.Error(t, err)
}

func TestGeneratePdfPullSlip_TemplateEmpty(t *testing.T) {
	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{
		ID: "REQ-X",
		RequesterReqID: pgtype.Text{
			String: "REQ-X",
			Valid:  true,
		},
		RequesterSymbol: pgtype.Text{String: "empty", Valid: true},
	}
	_, err := svc.GeneratePdfPullSlip(appCtx, pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.Error(t, err)
}

func TestGeneratePdfPullSlip_TemplateDbError(t *testing.T) {
	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{
		ID: "REQ-X",
		RequesterReqID: pgtype.Text{
			String: "REQ-X",
			Valid:  true,
		},
		RequesterSymbol: pgtype.Text{String: "error", Valid: true},
	}
	_, err := svc.GeneratePdfPullSlip(appCtx, pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.Error(t, err)
}

// ── GeneratePdfPullSlipForPrs ─────────────────────────────────────────────────

type mockPrRepo struct {
	pr_db.PrRepo // embed to satisfy the full interface
	notes        []pr_db.Notification
	conditions   []pr_db.Notification
	noteErr      error
	condErr      error
}

func (m *mockPrRepo) GetNotificationsByPrId(_ common.ExtendedContext, params pr_db.GetNotificationsByPrIdParams) ([]pr_db.Notification, int64, error) {
	if params.Kind == string(pr_db.NotificationKindNote) {
		return m.notes, int64(len(m.notes)), m.noteErr
	}
	return m.conditions, int64(len(m.conditions)), m.condErr
}

func (m *mockPrRepo) GetTemplateByPurposeAudienceLabelAndOwner(_ common.ExtendedContext, params pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams) (pr_db.Template, error) {
	if params.Owner == "invalid" {
		return pr_db.Template{Body: "{{.Unclosed"}, nil
	}
	if params.Owner == "empty" {
		return pr_db.Template{}, nil
	}
	if params.Owner == "error" {
		return pr_db.Template{}, errors.New("template db error")
	}
	return pr_db.Template{}, pgx.ErrNoRows
}

func newSvcWithMock(repo pr_db.PrRepo) *PdfServiceImpl {
	return &PdfServiceImpl{prRepo: repo}
}

func TestGeneratePdfPullSlipForPrs_Single(t *testing.T) {
	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{
		ID:             "pr-1",
		RequesterReqID: pgtype.Text{String: "REQ-1", Valid: true},
	}
	pdfBytes, err := svc.GeneratePdfPullSlipForPrs(appCtx, []pr_db.PatronRequest{pr})
	assert.NoError(t, err)
	assert.True(t, len(pdfBytes) > 0)
}

func TestGeneratePdfPullSlipForPrs_Multiple(t *testing.T) {
	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
	prs := []pr_db.PatronRequest{
		{ID: "pr-1", RequesterReqID: pgtype.Text{String: "REQ-1", Valid: true}},
		{ID: "pr-2", RequesterReqID: pgtype.Text{String: "REQ-2", Valid: true}},
	}
	pdfBytes, err := svc.GeneratePdfPullSlipForPrs(appCtx, prs)
	assert.NoError(t, err)
	assert.True(t, len(pdfBytes) > 0)
}

func TestGeneratePdfPullSlipForPrs_NoteError(t *testing.T) {
	repo := &mockPrRepo{noteErr: errors.New("note db error")}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{ID: "pr-1", RequesterReqID: pgtype.Text{String: "REQ-1", Valid: true}}
	_, err := svc.GeneratePdfPullSlipForPrs(appCtx, []pr_db.PatronRequest{pr})
	assert.Error(t, err)
}

func TestGeneratePdfPullSlipForPrs_ConditionError(t *testing.T) {
	repo := &mockPrRepo{condErr: errors.New("condition db error")}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{ID: "pr-1", RequesterReqID: pgtype.Text{String: "REQ-1", Valid: true}}
	_, err := svc.GeneratePdfPullSlipForPrs(appCtx, []pr_db.PatronRequest{pr})
	assert.Error(t, err)
}

// ── ServiceInfo edge cases ────────────────────────────────────────────────────

func TestGeneratePdfPullSlip_ServiceInfoEmptyServiceLevel(t *testing.T) {
	repo := &mockPrRepo{}
	svc := newSvcWithMock(repo)
	pr := pr_db.PatronRequest{
		RequesterReqID: pgtype.Text{String: "REQ-SVC", Valid: true},
		IllRequest: iso18626.Request{
			ServiceInfo: &iso18626.ServiceInfo{
				ServiceType:  iso18626.TypeServiceTypeCopy,
				ServiceLevel: &iso18626.TypeSchemeValuePair{Text: ""},
			},
		},
	}
	pdfBytes, err := svc.GeneratePdfPullSlip(appCtx, pr, nil, nil)
	assert.NoError(t, err)
	assert.NotEmpty(t, pdfBytes)
}

// ── RenderPdfPullSlip ─────────────────────────────────────────────────────────

func pdfText(t *testing.T, pdf []byte) string {
	t.Helper()
	parsed, err := reader.Parse(pdf)
	require.NoError(t, err)
	var text strings.Builder
	for i := range parsed.PageCount() {
		page, err := parsed.Page(i)
		require.NoError(t, err)
		pageText, err := page.ExtractText()
		require.NoError(t, err)
		text.WriteString(pageText)
	}
	return text.String()
}

func TestRenderPdfPullSlip_EverySampleWithDefaultPreset(t *testing.T) {
	preset, err := prservice.GetStateModelTemplateDefault(proapi.Pullslip, proapi.TemplateAudienceStaff, "pullslip-pdf")
	require.NoError(t, err)
	want := map[string][]string{
		"typical":   {"REQ-2026-00417", "QA76.6", "2026-08-15", "Library use only"},
		"long":      {"US-REQ-CONSORTIUM-2026-0000419", "Z 105.5"},
		"sparse":    {"REQ-7", email.DEFAULT_FOR_NO_VALUE},
		"requester": {"REQ-2026-00418", "Silent Spring", "2026-09-01"},
	}
	samples, err := prservice.GetTemplateSamples()
	require.NoError(t, err)
	require.Len(t, samples, len(want))
	for _, sample := range samples {
		t.Run(sample.Name, func(t *testing.T) {
			pdf, err := RenderPdfPullSlip(sample.Request, sample.Notes, sample.Conditions, preset.Labels, preset.Body)
			require.NoError(t, err)
			text := pdfText(t, pdf)
			require.Contains(t, want, sample.Name)
			for _, expected := range want[sample.Name] {
				assert.Contains(t, text, expected)
			}
		})
	}
}

func TestMergePdfs_KeepsOrder(t *testing.T) {
	var pdfs [][]byte
	for _, title := range []string{"FIRST-SLIP", "SECOND-SLIP"} {
		pdf, err := RenderPdfPullSlip(pr_db.PatronRequest{RequesterReqID: pgtype.Text{String: "REQ-1", Valid: true}}, nil, nil, nil, "<p>"+title+"</p>")
		require.NoError(t, err)
		pdfs = append(pdfs, pdf)
	}
	merged, err := MergePdfs(pdfs)
	require.NoError(t, err)
	text := pdfText(t, merged)
	first, second := strings.Index(text, "FIRST-SLIP"), strings.Index(text, "SECOND-SLIP")
	assert.True(t, first >= 0 && second > first, text)
}

func TestMergePdfs_InvalidPdf(t *testing.T) {
	_, err := MergePdfs([][]byte{[]byte("not a pdf")})
	assert.Error(t, err)
}

func TestRenderPdfPullSlip_TemplateError(t *testing.T) {
	_, err := RenderPdfPullSlip(pr_db.PatronRequest{RequesterReqID: pgtype.Text{String: "REQ-1", Valid: true}}, nil, nil, nil, "<p>\n{{.Title}\n</p>")
	assert.ErrorContains(t, err, "template:2:")
}
