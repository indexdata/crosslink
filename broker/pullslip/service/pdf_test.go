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

	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
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
	pdfBytes, err := svc.GeneratePdfPullSlip(pr, []pr_db.Notification{}, []pr_db.Notification{})
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
	pdfBytes, err := svc.GeneratePdfPullSlip(pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.NoError(t, err)
	assert.NotEmpty(t, pdfBytes)
	assert.Equal(t, "%PDF", string(pdfBytes[:4]))
}

func TestGeneratePdfPullSlip_FullData(t *testing.T) {
	callNumber := "QA76.9.A25"
	dueDate := utils.XSDDateTime{Time: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)}

	svc := &PdfService{}
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

	pdfBytes, err := svc.GeneratePdfPullSlip(pr, notes, conditions)
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
		RequesterReqID: pgtype.Text{
			String: "\x80invalid", // non-ASCII causes code128 encoding to fail
			Valid:  true,
		},
	}
	_, err := svc.GeneratePdfPullSlip(pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.Error(t, err)
}

func TestGeneratePdfPullSlip_TemplateError(t *testing.T) {
	orig := pullSlipTemplate
	defer func() { pullSlipTemplate = orig }()
	pullSlipTemplate = `{{.Unclosed`

	svc := &PdfService{}
	pr := pr_db.PatronRequest{
		ID: "REQ-X",
		RequesterReqID: pgtype.Text{
			String: "REQ-X",
			Valid:  true,
		},
	}
	_, err := svc.GeneratePdfPullSlip(pr, []pr_db.Notification{}, []pr_db.Notification{})
	assert.Error(t, err)
}

// ── formatPhysicalAddress ─────────────────────────────────────────────────────

func TestFormatPhysicalAddress_Full(t *testing.T) {
	a := &iso18626.PhysicalAddress{
		Line1:      "1 Main St",
		Line2:      "Floor 2",
		Locality:   "Springfield",
		PostalCode: "12345",
		Region:     &iso18626.TypeSchemeValuePair{Text: "IL"},
		Country:    &iso18626.TypeSchemeValuePair{Text: "US"},
	}
	assert.Equal(t, "1 Main St, Floor 2, Springfield, 12345, IL, US", formatPhysicalAddress(a))
}

func TestFormatPhysicalAddress_Partial(t *testing.T) {
	// Only Line1 and Locality — Region/Country nil, Line2/PostalCode empty
	a := &iso18626.PhysicalAddress{
		Line1:    "42 Book Rd",
		Locality: "Shelbyville",
	}
	assert.Equal(t, "42 Book Rd, Shelbyville", formatPhysicalAddress(a))
}

func TestFormatPhysicalAddress_EmptyRegionText(t *testing.T) {
	// Region present but empty Text — should be skipped
	a := &iso18626.PhysicalAddress{
		Line1:   "1 St",
		Region:  &iso18626.TypeSchemeValuePair{Text: ""},
		Country: &iso18626.TypeSchemeValuePair{Text: ""},
	}
	assert.Equal(t, "1 St", formatPhysicalAddress(a))
}

func TestFormatPhysicalAddress_Empty(t *testing.T) {
	assert.Equal(t, "", formatPhysicalAddress(&iso18626.PhysicalAddress{}))
}

// ── getStaffNotes ─────────────────────────────────────────────────────────────

func TestGetStaffNotes_Empty(t *testing.T) {
	assert.Equal(t, DEFAULT_FOR_NO_VALUE, getStaffNotes([]pr_db.Notification{}))
}

func TestGetStaffNotes_InvalidNotesSkipped(t *testing.T) {
	notes := []pr_db.Notification{
		{Note: pgtype.Text{String: "valid note", Valid: true}},
		{Note: pgtype.Text{String: "ignored", Valid: false}},
	}
	assert.Equal(t, "valid note", getStaffNotes(notes))
}

func TestGetStaffNotes_Multiple(t *testing.T) {
	notes := []pr_db.Notification{
		{Note: pgtype.Text{String: "note one", Valid: true}},
		{Note: pgtype.Text{String: "note two", Valid: true}},
	}
	assert.Equal(t, "note one\nnote two", getStaffNotes(notes))
}

// ── getLoanConditions ─────────────────────────────────────────────────────────

func TestGetLoanConditions_Empty(t *testing.T) {
	assert.Equal(t, DEFAULT_FOR_NO_VALUE, getLoanConditions([]pr_db.Notification{}))
}

func TestGetLoanConditions_InvalidSkipped(t *testing.T) {
	conditions := []pr_db.Notification{
		{Condition: pgtype.Text{String: "library use only", Valid: true}},
		{Condition: pgtype.Text{String: "ignored", Valid: false}},
	}
	assert.Equal(t, "library use only", getLoanConditions(conditions))
}

func TestGetLoanConditions_Multiple(t *testing.T) {
	conditions := []pr_db.Notification{
		{Condition: pgtype.Text{String: "no photocopying", Valid: true}},
		{Condition: pgtype.Text{String: "in-library use", Valid: true}},
	}
	assert.Equal(t, "no photocopying\nin-library use", getLoanConditions(conditions))
}

// ── getCallNumber ─────────────────────────────────────────────────────────────

func TestGetCallNumber_Empty(t *testing.T) {
	assert.Equal(t, DEFAULT_FOR_NO_VALUE, getCallNumber(pr_db.PatronRequest{}))
}

func TestGetCallNumber_NilCallNumber(t *testing.T) {
	pr := pr_db.PatronRequest{Items: []pr_db.PrItem{{ID: "i1", CallNumber: nil}}}
	assert.Equal(t, DEFAULT_FOR_NO_VALUE, getCallNumber(pr))
}

func TestGetCallNumber_EmptyCallNumber(t *testing.T) {
	empty := ""
	pr := pr_db.PatronRequest{Items: []pr_db.PrItem{{ID: "i1", CallNumber: &empty}}}
	assert.Equal(t, DEFAULT_FOR_NO_VALUE, getCallNumber(pr))
}

func TestGetCallNumber_Multiple(t *testing.T) {
	cn1, cn2 := "QA76", "PR9199"
	pr := pr_db.PatronRequest{Items: []pr_db.PrItem{
		{ID: "i1", CallNumber: &cn1},
		{ID: "i2", CallNumber: &cn2},
	}}
	assert.Equal(t, "QA76, PR9199", getCallNumber(pr))
}

// ── getPickupLocation ─────────────────────────────────────────────────────────

func TestGetPickupLocation_NoDeliveryInfo(t *testing.T) {
	assert.Equal(t, DEFAULT_FOR_NO_VALUE, getPickupLocation(pr_db.PatronRequest{}))
}

func TestGetPickupLocation_NilAddress(t *testing.T) {
	pr := pr_db.PatronRequest{
		IllRequest: iso18626.Request{
			RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{{Address: nil}},
		},
	}
	assert.Equal(t, DEFAULT_FOR_NO_VALUE, getPickupLocation(pr))
}

func TestGetPickupLocation_PhysicalAddress(t *testing.T) {
	pr := pr_db.PatronRequest{
		IllRequest: iso18626.Request{
			RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{
				{Address: &iso18626.Address{
					PhysicalAddress: &iso18626.PhysicalAddress{Line1: "Pickup Desk"},
				}},
			},
		},
	}
	assert.Equal(t, "Pickup Desk", getPickupLocation(pr))
}

func TestGetPickupLocation_ElectronicAddress(t *testing.T) {
	pr := pr_db.PatronRequest{
		IllRequest: iso18626.Request{
			RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{
				{Address: &iso18626.Address{
					ElectronicAddress: &iso18626.ElectronicAddress{
						ElectronicAddressData: "patron@library.org",
					},
				}},
			},
		},
	}
	assert.Equal(t, "patron@library.org", getPickupLocation(pr))
}

func TestGetPickupLocation_AddressWithNoUsableFields(t *testing.T) {
	// Address present but neither PhysicalAddress nor a non-empty ElectronicAddressData
	pr := pr_db.PatronRequest{
		IllRequest: iso18626.Request{
			RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{
				{Address: &iso18626.Address{}},
			},
		},
	}
	assert.Equal(t, DEFAULT_FOR_NO_VALUE, getPickupLocation(pr))
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

func newSvcWithMock(repo pr_db.PrRepo) *PdfService {
	return &PdfService{prRepo: repo}
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
	svc := &PdfService{}
	pr := pr_db.PatronRequest{
		RequesterReqID: pgtype.Text{String: "REQ-SVC", Valid: true},
		IllRequest: iso18626.Request{
			ServiceInfo: &iso18626.ServiceInfo{
				ServiceType:  iso18626.TypeServiceTypeCopy,
				ServiceLevel: &iso18626.TypeSchemeValuePair{Text: ""},
			},
		},
	}
	pdfBytes, err := svc.GeneratePdfPullSlip(pr, nil, nil)
	assert.NoError(t, err)
	assert.NotEmpty(t, pdfBytes)
}
