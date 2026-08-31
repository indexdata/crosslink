package sched_service

import (
	"errors"
	"strings"
	"testing"

	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/email"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	psservice "github.com/indexdata/crosslink/broker/pullslip/service"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

// mockEmailPrRepo implements pr_db.PrRepo for email sender tests.
// Only ListPatronRequests is overridden; all other methods panic via the nil embed.
type mockEmailPrRepo struct {
	pr_db.PrRepo
	listResult     []pr_db.PatronRequest
	fullCount      int64
	listErr        error
	listCalled     bool
	gotParams      pr_db.ListPatronRequestsParams
	gotQuery       pgcql.Query
	template       pr_db.Template
	templateErr    error
	templateCalled bool
	gotTemplate    pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams
}

func (m *mockEmailPrRepo) ListPatronRequests(_ common.ExtendedContext, params pr_db.ListPatronRequestsParams, query pgcql.Query) ([]pr_db.PatronRequest, int64, error) {
	m.listCalled = true
	m.gotParams = params
	m.gotQuery = query
	if m.fullCount != 0 {
		return m.listResult, m.fullCount, m.listErr
	}
	return m.listResult, int64(len(m.listResult)), m.listErr
}

func (m *mockEmailPrRepo) GetTemplateByPurposeAudienceLabelAndOwner(_ common.ExtendedContext, params pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams) (pr_db.Template, error) {
	m.templateCalled = true
	m.gotTemplate = params
	if m.templateErr != nil {
		return pr_db.Template{}, m.templateErr
	}
	if m.template.ID != "" {
		return m.template, nil
	}
	return validEmailTemplate(), nil
}

// mockEmailIllRepo implements the owner lookup needed to resolve the sender address.
type mockEmailIllRepo struct {
	ill_db.IllRepo
	fromEmail string
	err       error
}

func (m *mockEmailIllRepo) GetPeerBySymbol(_ common.ExtendedContext, _ string) (ill_db.Peer, error) {
	if m.err != nil {
		return ill_db.Peer{}, m.err
	}
	return ill_db.Peer{CustomData: dirapi.Entry{FromEmail: &m.fromEmail}}, nil
}

// mockEmailService records the raw message bytes passed to SendMail.
type mockEmailService struct {
	err    error
	called bool
	data   []byte
	ready  bool
}

func (m *mockEmailService) IsReadyToSend() bool {
	return m.ready
}

func (m *mockEmailService) SendEmail(from string, to []string, raw []byte) error {
	m.called = true
	m.data = append([]byte(nil), raw...)
	return m.err
}

// mockPdfGen implements PdfGenerator.
type mockPdfGen struct {
	data []byte
	err  error
}

func (m *mockPdfGen) GeneratePdfPullSlipForPrs(_ common.ExtendedContext, _ []pr_db.PatronRequest) ([]byte, error) {
	return m.data, m.err
}

// ---------------------------------------------------------------------------
// Shared test fixtures
// ---------------------------------------------------------------------------

func validEmailCustomData() map[string]any {
	return map[string]any{
		"to":            []string{"user@example.com"},
		"templateLabel": "pullslips",
	}
}

func validEmailTemplate() pr_db.Template {
	return pr_db.Template{
		ID:          "template-id",
		Owner:       "ISIL:OWNER",
		Purpose:     "email",
		Subject:     pgtype.Text{String: "Test Subject", Valid: true},
		Body:        "Test body text",
		ContentType: "text",
	}
}

func validEmailEvent() events.Event {
	return events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{
					Selector: "cql.allRecords=1",
					Owner:    "ISIL:OWNER",
				},
			},
			CustomData: validEmailCustomData(),
		},
	}
}

// newEmailSvc creates an EmailSenderService wired to the supplied mocks.
func newEmailSvc(prRepo pr_db.PrRepo, emailService email.EmailService, pdf psservice.PdfService) *EmailSenderService {
	return EmailSenderServiceWithClient(prRepo, &mockEmailIllRepo{fromEmail: "from@example.com"}, emailService, pdf)
}

// ---------------------------------------------------------------------------
// extractEmailData
// ---------------------------------------------------------------------------

func TestExtractEmailData_NilCustomData(t *testing.T) {
	_, err := extractEmailData(events.EventData{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "customData is nil")
}

func TestExtractEmailData_MissingTo(t *testing.T) {
	_, err := extractEmailData(events.EventData{
		CustomData: map[string]any{"templateLabel": "pullslips"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestExtractEmailData_ToAsStringSlice(t *testing.T) {
	ed, err := extractEmailData(events.EventData{
		CustomData: map[string]any{
			"to":            []string{"a@b.com", "c@d.com"},
			"templateLabel": "pullslips",
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"a@b.com", "c@d.com"}, ed.To)
	assert.Equal(t, "pullslips", ed.TemplateLabel)
}

func TestExtractEmailData_ToAsInterfaceSlice(t *testing.T) {
	ed, err := extractEmailData(events.EventData{
		CustomData: map[string]any{
			"to":            []interface{}{"x@y.com"},
			"templateLabel": "pullslips",
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"x@y.com"}, ed.To)
}

func TestExtractEmailData_ToAsInterfaceSlice_NonString(t *testing.T) {
	_, err := extractEmailData(events.EventData{
		CustomData: map[string]any{
			"to": []interface{}{42},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "non-string value")
}

func TestExtractEmailData_ToUnexpectedType(t *testing.T) {
	_, err := extractEmailData(events.EventData{
		CustomData: map[string]any{
			"to": 12345,
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected type")
}

func TestExtractEmailData_AllOptionalFields(t *testing.T) {
	ed, err := extractEmailData(events.EventData{
		CustomData: map[string]any{
			"to":            []string{"a@b.com"},
			"templateLabel": "pullslips",
			"includePdf":    true,
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, "pullslips", ed.TemplateLabel)
	assert.True(t, ed.IncludePdf)
}

// ---------------------------------------------------------------------------
// generateAndEmailPullslip
// ---------------------------------------------------------------------------

func TestGenerateAndEmailPullslip_NilBatchActionData(t *testing.T) {
	svc := newEmailSvc(&mockEmailPrRepo{}, &mockEmailService{}, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, events.Event{})
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
}

func TestGenerateAndEmailPullslip_EmptySelector(t *testing.T) {
	svc := newEmailSvc(&mockEmailPrRepo{}, &mockEmailService{}, nil)
	event := events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{Selector: ""},
			},
		},
	}
	status, _ := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
}

func TestGenerateAndEmailPullslip_InvalidCQL(t *testing.T) {
	svc := newEmailSvc(&mockEmailPrRepo{}, &mockEmailService{}, nil)
	event := events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{Selector: "unknownFieldXYZ = test", Owner: "ISIL:OWNER"},
			},
		},
	}
	status, result := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
}

func TestGenerateAndEmailPullslip_NilCustomData(t *testing.T) {
	svc := newEmailSvc(&mockEmailPrRepo{}, &mockEmailService{}, nil)
	event := events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{Selector: "cql.allRecords=1", Owner: "ISIL:OWNER"},
			},
			// CustomData intentionally nil
		},
	}
	status, _ := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
}

func TestGenerateAndEmailPullslip_EmptyTo(t *testing.T) {
	svc := newEmailSvc(&mockEmailPrRepo{}, &mockEmailService{}, nil)
	event := events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{Selector: "cql.allRecords=1", Owner: "ISIL:OWNER"},
			},
			CustomData: map[string]any{
				"to": []string{}, "templateLabel": "pullslips",
			},
		},
	}
	status, result := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
}

func TestGenerateAndEmailPullslip_MissingTemplateLabel(t *testing.T) {
	svc := newEmailSvc(&mockEmailPrRepo{}, &mockEmailService{}, nil)
	event := events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{Selector: "cql.allRecords=1", Owner: "ISIL:OWNER"},
			},
			CustomData: map[string]any{
				"to": []string{"a@b.com"},
			},
		},
	}
	status, _ := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
}

func TestGenerateAndEmailPullslip_TemplateLookupError(t *testing.T) {
	prRepo := &mockEmailPrRepo{templateErr: errors.New("template not found")}
	svc := newEmailSvc(prRepo, &mockEmailService{}, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.True(t, prRepo.templateCalled)
}

func TestGenerateAndEmailPullslip_TemplateEmptySubject(t *testing.T) {
	prRepo := &mockEmailPrRepo{template: pr_db.Template{
		ID:          "template-id",
		Subject:     pgtype.Text{},
		Body:        "Body",
		ContentType: "text",
	}}
	svc := newEmailSvc(prRepo, &mockEmailService{}, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
}

func TestGenerateAndEmailPullslip_TemplateInvalidBody(t *testing.T) {
	prRepo := &mockEmailPrRepo{
		template: pr_db.Template{
			ID: "template-id",
			Subject: pgtype.Text{
				Valid:  true,
				String: "Subject",
			},
			Body:        "Body {{.Invalid text",
			ContentType: "text",
		},
		listResult: []pr_db.PatronRequest{{ID: "pr-1"}},
	}
	svc := newEmailSvc(prRepo, &mockEmailService{}, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.Equal(t, "failed to render email body", result.EventError.Message)
}

func TestGenerateAndEmailPullslip_TemplateInvalidSubject(t *testing.T) {
	prRepo := &mockEmailPrRepo{
		template: pr_db.Template{
			ID: "template-id",
			Subject: pgtype.Text{
				Valid:  true,
				String: "Subject {{.Invalid text",
			},
			Body:        "Body",
			ContentType: "text",
		},
		listResult: []pr_db.PatronRequest{{ID: "pr-1"}},
	}
	svc := newEmailSvc(prRepo, &mockEmailService{}, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.Equal(t, "failed to render email subject", result.EventError.Message)
}

func TestGenerateAndEmailPullslip_TemplateEmptyBody(t *testing.T) {
	prRepo := &mockEmailPrRepo{template: pr_db.Template{
		ID:          "template-id",
		Subject:     pgtype.Text{String: "Subject", Valid: true},
		Body:        "",
		ContentType: "text",
	}}
	svc := newEmailSvc(prRepo, &mockEmailService{}, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
}

func TestGenerateAndEmailPullslip_ListPatronRequestsError(t *testing.T) {
	prRepo := &mockEmailPrRepo{listErr: errors.New("db down")}
	svc := newEmailSvc(prRepo, &mockEmailService{}, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
}

func TestGenerateAndEmailPullslip_SMTPError(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{{ID: "pr-1"}}}
	mailer := &mockEmailService{err: errors.New("SMTP unavailable")}
	svc := newEmailSvc(prRepo, mailer, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.True(t, mailer.called)
}

func TestGenerateAndEmailPullslip_SuccessNoEmail(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
	mailer := &mockEmailService{}
	svc := newEmailSvc(prRepo, mailer, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, "no patron requests matched the selector", result.Note)
	assert.False(t, mailer.called)
}

func TestGenerateAndEmailPullslip_SuccessEmailWithZero(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
	mailer := &mockEmailService{}
	svc := newEmailSvc(prRepo, mailer, nil)
	event := validEmailEvent()
	event.EventData.CustomData["sendEmpty"] = true
	status, result := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, result)
	assert.True(t, mailer.called)

	assert.True(t, strings.Contains(string(mailer.data), "user@example.com"))
	assert.Equal(t, pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams{
		Owner:    "ISIL:OWNER",
		Purpose:  "email",
		Label:    "pullslips",
		Audience: "staff",
	}, prRepo.gotTemplate)
}

func TestGenerateAndEmailPullslip_Success(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{{ID: "pr-1"}}}
	mailer := &mockEmailService{}
	svc := newEmailSvc(prRepo, mailer, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, result)
	assert.True(t, mailer.called)

	assert.True(t, strings.Contains(string(mailer.data), "user@example.com"))
	assert.Equal(t, pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams{
		Owner:    "ISIL:OWNER",
		Purpose:  "email",
		Label:    "pullslips",
		Audience: "staff",
	}, prRepo.gotTemplate)
}

func TestGenerateAndEmailPullslip_PerformsPlaceholderSubstitution(t *testing.T) {
	prRepo := &mockEmailPrRepo{
		listResult: []pr_db.PatronRequest{{ID: "pr-1"}, {ID: "pr-2"}},
		fullCount:  5,
		template: pr_db.Template{
			ID:          "template-id",
			Subject:     pgtype.Text{String: "Selected {{.FullCount}}", Valid: true},
			Body:        "Attached {{.ActualCount}} of {{.FullCount}} from {{.BatchQuery}}",
			ContentType: "text",
		},
	}
	mailer := &mockEmailService{}
	svc := newEmailSvc(prRepo, mailer, nil)

	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, result)
	message := string(mailer.data)
	assert.Contains(t, message, "Selected 5")
	assert.Contains(t, message, "Attached 2 of 5 from cql.allRecords=3D1")
}

func TestGenerateAndEmailPullslip_HtmlTemplate(t *testing.T) {
	prRepo := &mockEmailPrRepo{
		listResult: []pr_db.PatronRequest{{ID: "pr-1"}},
		template: pr_db.Template{
			ID:          "template-id",
			Subject:     pgtype.Text{String: "Subject", Valid: true},
			Body:        "<p>Body</p>",
			ContentType: "html",
		}}
	mailer := &mockEmailService{}
	svc := newEmailSvc(prRepo, mailer, nil)

	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, result)
	assert.Contains(t, string(mailer.data), "Content-Type: text/html")
}

func TestGenerateAndEmailPullslip_WithPDF_Success(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{{ID: "pr-1"}}}
	mailer := &mockEmailService{}
	pdf := &mockPdfGen{data: []byte("%PDF fake")}
	svc := newEmailSvc(prRepo, mailer, pdf)

	event := validEmailEvent()
	event.EventData.CustomData["includePdf"] = true

	status, result := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, result)
	assert.True(t, mailer.called)
	assert.Contains(t, string(mailer.data), "application/pdf")
}

func TestGenerateAndEmailPullslip_WithPDF_NilGenerator(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{{ID: "pr-1"}}}
	mailer := &mockEmailService{}
	// pdf generator is nil — IncludePdf=true must return an error, not panic.
	svc := newEmailSvc(prRepo, mailer, nil)

	event := validEmailEvent()
	event.EventData.CustomData["includePdf"] = true

	status, result := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.False(t, mailer.called)
}

func TestGenerateAndEmailPullslip_WithPDF_GenerateError(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{{ID: "pr-1"}}}
	mailer := &mockEmailService{}
	pdf := &mockPdfGen{err: errors.New("pdf engine failure")}
	svc := newEmailSvc(prRepo, mailer, pdf)

	event := validEmailEvent()
	event.EventData.CustomData["includePdf"] = true

	status, result := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.False(t, mailer.called)
}

// ---------------------------------------------------------------------------
// EmailPullslip
// ---------------------------------------------------------------------------

func TestEmailPullslip_WhenReadyToSend_SendsEmail(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{{ID: "pr-1"}}}
	mailer := &mockEmailService{ready: true}
	svc := EmailSenderServiceWithClient(prRepo, &mockEmailIllRepo{fromEmail: "from@example.com"}, mailer, nil)

	status, _ := svc.EmailPullslip(testCtx, validEmailEvent())

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.True(t, mailer.called)
}

func TestEmailPullslip_DoesNotPanic(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
	mailer := &mockEmailService{ready: false}
	svc := EmailSenderServiceWithClient(prRepo, &mockEmailIllRepo{fromEmail: "from@example.com"}, mailer, nil)

	// EmailPullslip ignores the ProcessTask error (_, _ = ...); verify no panic.
	svc.EmailPullslip(testCtx, validEmailEvent())
}

func TestEmailPullslip_InvalidEvent_ErrorStatus(t *testing.T) {
	svc := EmailSenderServiceWithClient(nil, &mockEmailIllRepo{fromEmail: "from@example.com"}, &mockEmailService{}, nil)

	// Event with no BatchActionData → handler returns error status.
	status, _ := svc.EmailPullslip(testCtx, events.Event{})

	assert.Equal(t, events.EventStatusError, status)
}

func TestEmailPullslip_SetEventToFailed(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
	mailer := &mockEmailService{}
	svc := EmailSenderServiceWithClient(prRepo, &mockEmailIllRepo{fromEmail: "from@example.com"}, mailer, nil)

	status, _ := svc.EmailPullslip(testCtx, validEmailEvent())

	assert.Equal(t, events.EventStatusError, status)
	assert.False(t, mailer.called)
}
