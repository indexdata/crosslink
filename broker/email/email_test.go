package email

import (
	"strings"
	"testing"
	"time"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// joinAddresses
// ---------------------------------------------------------------------------

func TestJoinAddresses_Empty(t *testing.T) {
	assert.Equal(t, "", joinAddresses(nil))
	assert.Equal(t, "", joinAddresses([]string{}))
}

func TestJoinAddresses_Single(t *testing.T) {
	assert.Equal(t, "a@b.com", joinAddresses([]string{"a@b.com"}))
}

func TestJoinAddresses_Multiple(t *testing.T) {
	assert.Equal(t, "a@b.com, c@d.com, e@f.com",
		joinAddresses([]string{"a@b.com", "c@d.com", "e@f.com"}))
}

// ---------------------------------------------------------------------------
// buildRawMessage
// ---------------------------------------------------------------------------

func TestBuildRawMessage_PlainTextHeaders(t *testing.T) {
	data := EmailData{
		To:      []string{"to@example.com"},
		Subject: "Hello",
		Body:    "Plain text body",
	}
	raw, err := BuildRawMessage("from@example.com", data, nil)
	assert.NoError(t, err)
	msg := string(raw)
	assert.Contains(t, msg, "From: from@example.com")
	assert.Contains(t, msg, "To: to@example.com")
	assert.Contains(t, msg, "Subject:")
	assert.Contains(t, msg, "MIME-Version: 1.0")
	assert.Contains(t, msg, "text/plain")
	assert.NotContains(t, msg, "application/pdf")
}

func TestBuildRawMessage_HTMLBody(t *testing.T) {
	data := EmailData{
		To:     []string{"to@example.com"},
		Body:   "<p>HTML body</p>",
		IsHTML: true,
	}
	raw, err := BuildRawMessage("from@example.com", data, nil)
	assert.NoError(t, err)
	assert.Contains(t, string(raw), "text/html")
}

func TestBuildRawMessage_MultipleRecipients(t *testing.T) {
	data := EmailData{
		To:   []string{"a@b.com", "c@d.com"},
		Body: "body",
	}
	raw, err := BuildRawMessage("from@example.com", data, nil)
	assert.NoError(t, err)
	assert.Contains(t, string(raw), "a@b.com, c@d.com")
}

func TestBuildRawMessage_WithPDFAttachment(t *testing.T) {
	data := EmailData{
		To:   []string{"to@example.com"},
		Body: "body with attachment",
	}
	att := &PdfAttach{Filename: "pull-slips.pdf", Data: []byte("%PDF-1.4 fake")}
	raw, err := BuildRawMessage("from@example.com", data, att)
	assert.NoError(t, err)
	msg := string(raw)
	assert.Contains(t, msg, "application/pdf")
	assert.Contains(t, msg, `attachment; filename="pull-slips.pdf"`)
	assert.Contains(t, msg, "Content-Transfer-Encoding: base64")
}

func TestBuildRawMessage_WithoutAttachment(t *testing.T) {
	data := EmailData{To: []string{"to@example.com"}, Body: "body"}
	raw, err := BuildRawMessage("from@example.com", data, nil)
	assert.NoError(t, err)
	assert.NotContains(t, string(raw), "application/pdf")
}

// ---------------------------------------------------------------------------
// RenderPullSlipHTMLWithTemplate
// ---------------------------------------------------------------------------

func TestRenderPullSlipHTML(t *testing.T) {
	template := "<div class=\"section\">\n              <b>Service Type:</b> {{.ServiceType}} <br/>\n              <b>Service Level:</b> {{.ServiceLevel}} <br/>\n              <b>System Identifier:</b> {{.SystemIdentifier}} <br/>\n              <b>Title:</b> {{.Title}} <br/>\n              <b>Author:</b> {{.Author}} <br/>\n              <b>Publisher:</b> {{.Publisher}} <br/>\n              <b>Volume(s):</b> {{.Volume}} <br/>\n              <b>Issue:</b> {{.Issue}} <br/>\n              <b>Pages:</b> {{.Pages}} <br/>\n          </div>"
	html, err := RenderPullSlipHTMLWithTemplate(PullSlipData{
		ServiceType:      "Loan",
		Title:            "Big Shark",
		Author:           "John Doe",
		DueDate:          "2026-01-01",
		ReturnAddress:    "1 Test Street",
		SystemIdentifier: "abc123",
	}, template)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(html, "Loan"))
	assert.True(t, strings.Contains(html, "John Doe"))
	assert.True(t, strings.Contains(html, "Big Shark"))
	assert.True(t, strings.Contains(html, "abc123"))
}

func TestRenderPullSlipHTML_UsesProvidedTemplate(t *testing.T) {
	html, err := RenderPullSlipHTMLWithTemplate(PullSlipData{ReqId: "REQ-1"}, "<main>{{.ReqId}}</main>")

	assert.NoError(t, err)
	assert.Equal(t, "<main>REQ-1</main>", html)
}

func TestRenderPullSlipHTML_InvalidTemplate(t *testing.T) {
	_, err := RenderPullSlipHTMLWithTemplate(PullSlipData{ReqId: "X"}, "{{.Unclosed")
	assert.Error(t, err)
}

func TestRenderPullSlipHTML_ExecuteError(t *testing.T) {
	_, err := RenderPullSlipHTMLWithTemplate(PullSlipData{ReqId: "X"}, "{{index . \"nonexistent\"}}")
	// Execute on a struct with map-access fails
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// GetPullSlipData
// ---------------------------------------------------------------------------

func TestGetPullSlipData_PopulatesAllAvailableFields(t *testing.T) {
	callNumber := "QA76.73.G63"
	dueDate := utils.XSDDateTime{Time: time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)}
	pr := pr_db.PatronRequest{
		RequesterReqID: pgtype.Text{String: "REQ-123", Valid: true},
		Items:          []pr_db.PrItem{{ID: "item-1", CallNumber: &callNumber}},
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{
				Author:                 "Jane Doe",
				Title:                  "Distributed Libraries",
				Volume:                 "7",
				Issue:                  "2",
				EstimatedNoPages:       "18",
				SupplierUniqueRecordId: "SYS-456",
			},
			PublicationInfo: &iso18626.PublicationInfo{Publisher: "Index Press"},
			ServiceInfo: &iso18626.ServiceInfo{
				ServiceType:  iso18626.TypeServiceTypeLoan,
				ServiceLevel: &iso18626.TypeSchemeValuePair{Text: "Rush"},
			},
			RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{
				{Address: &iso18626.Address{
					PhysicalAddress: &iso18626.PhysicalAddress{
						Line1:      "Pickup Desk",
						Locality:   "Riga",
						PostalCode: "LV-1050",
						Country:    &iso18626.TypeSchemeValuePair{Text: "LV"},
					},
				}},
			},
			PatronInfo: &iso18626.PatronInfo{
				PatronId:  "P-789",
				GivenName: "Ann",
				Surname:   "Reader",
			},
		},
		IllResponse: iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{DueDate: &dueDate},
			ReturnInfo: &iso18626.ReturnInfo{PhysicalAddress: &iso18626.PhysicalAddress{
				Line1:    "Return Room",
				Line2:    "Shelf B",
				Locality: "Riga",
				Country:  &iso18626.TypeSchemeValuePair{Text: "LV"},
			}},
		},
	}
	notes := []pr_db.Notification{
		{Note: pgtype.Text{String: "first note", Valid: true}},
		{Note: pgtype.Text{String: "ignored note", Valid: false}},
		{Note: pgtype.Text{String: "second note", Valid: true}},
	}
	conditions := []pr_db.Notification{
		{Condition: pgtype.Text{String: "library use only", Valid: true}},
		{Condition: pgtype.Text{String: "no renewal", Valid: true}},
	}

	data := GetPullSlipData(pr, notes, conditions, "barcode-base64")

	assert.Equal(t, PullSlipData{
		BorrowerName:     "",
		ReqId:            "REQ-123",
		PickupLocation:   "Pickup Desk, Riga, LV-1050, LV",
		Title:            "Distributed Libraries",
		Author:           "Jane Doe",
		DueDate:          "2026-08-15",
		ReturnAddress:    "Return Room, Shelf B, Riga, LV",
		BarcodeBase64:    "barcode-base64",
		ServiceType:      "Loan",
		ServiceLevel:     "Rush",
		SystemIdentifier: "SYS-456",
		Publisher:        "Index Press",
		Volume:           "7",
		Issue:            "2",
		Pages:            "18",
		StaffNotes:       "first note\nsecond note",
		CallNumber:       "QA76.73.G63",
		LoanConditions:   "library use only\nno renewal",
		PatronName:       "Ann",
		PatronSurname:    "Reader",
		PatronId:         "P-789",
	}, data)
}

func TestGetPullSlipData_UsesDefaultsWhenOptionalFieldsAreMissing(t *testing.T) {
	data := GetPullSlipData(pr_db.PatronRequest{}, nil, nil, DEFAULT_FOR_NO_VALUE)

	assert.Equal(t, PullSlipData{
		BorrowerName:     "",
		ReqId:            "",
		PickupLocation:   DEFAULT_FOR_NO_VALUE,
		Title:            DEFAULT_FOR_NO_VALUE,
		Author:           DEFAULT_FOR_NO_VALUE,
		DueDate:          DEFAULT_FOR_NO_VALUE,
		ReturnAddress:    DEFAULT_FOR_NO_VALUE,
		BarcodeBase64:    DEFAULT_FOR_NO_VALUE,
		ServiceType:      DEFAULT_FOR_NO_VALUE,
		ServiceLevel:     DEFAULT_FOR_NO_VALUE,
		SystemIdentifier: DEFAULT_FOR_NO_VALUE,
		Publisher:        DEFAULT_FOR_NO_VALUE,
		Volume:           DEFAULT_FOR_NO_VALUE,
		Issue:            DEFAULT_FOR_NO_VALUE,
		Pages:            DEFAULT_FOR_NO_VALUE,
		StaffNotes:       DEFAULT_FOR_NO_VALUE,
		CallNumber:       DEFAULT_FOR_NO_VALUE,
		LoanConditions:   DEFAULT_FOR_NO_VALUE,
		PatronName:       DEFAULT_FOR_NO_VALUE,
		PatronSurname:    DEFAULT_FOR_NO_VALUE,
		PatronId:         DEFAULT_FOR_NO_VALUE,
	}, data)
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
