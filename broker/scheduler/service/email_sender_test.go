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
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

// mockEmailPrRepo implements pr_db.PrRepo for email sender tests.
// Only ListPatronRequests is overridden; all other methods panic via the nil embed.
type mockEmailPrRepo struct {
	pr_db.PrRepo
	listResult []pr_db.PatronRequest
	listErr    error
	listCalled bool
	gotParams  pr_db.ListPatronRequestsParams
	gotQuery   pgcql.Query
}

func (m *mockEmailPrRepo) ListPatronRequests(_ common.ExtendedContext, params pr_db.ListPatronRequestsParams, query pgcql.Query) ([]pr_db.PatronRequest, int64, error) {
	m.listCalled = true
	m.gotParams = params
	m.gotQuery = query
	return m.listResult, int64(len(m.listResult)), m.listErr
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
	return ill_db.Peer{CustomData: directory.Entry{FromEmail: &m.fromEmail}}, nil
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
		"to":      []string{"user@example.com"},
		"subject": "Test Subject",
		"body":    "Test body text",
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
		CustomData: map[string]any{"subject": "s", "body": "b"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestExtractEmailData_ToAsStringSlice(t *testing.T) {
	ed, err := extractEmailData(events.EventData{
		CustomData: map[string]any{
			"to":      []string{"a@b.com", "c@d.com"},
			"subject": "Subject",
			"body":    "Body",
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"a@b.com", "c@d.com"}, ed.To)
	assert.Equal(t, "Subject", ed.Subject)
	assert.Equal(t, "Body", ed.Body)
}

func TestExtractEmailData_ToAsInterfaceSlice(t *testing.T) {
	ed, err := extractEmailData(events.EventData{
		CustomData: map[string]any{
			"to":      []interface{}{"x@y.com"},
			"subject": "Sub",
			"body":    "Bdy",
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
			"to":         []string{"a@b.com"},
			"subject":    "Sub",
			"body":       "Bdy",
			"isHtml":     true,
			"includePdf": true,
		},
	})
	assert.NoError(t, err)
	assert.True(t, ed.IsHTML)
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
				"to": []string{}, "subject": "Sub", "body": "Body",
			},
		},
	}
	status, result := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
}

func TestGenerateAndEmailPullslip_EmptySubject(t *testing.T) {
	svc := newEmailSvc(&mockEmailPrRepo{}, &mockEmailService{}, nil)
	event := events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{Selector: "cql.allRecords=1", Owner: "ISIL:OWNER"},
			},
			CustomData: map[string]any{
				"to": []string{"a@b.com"}, "subject": "", "body": "Body",
			},
		},
	}
	status, _ := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
}

func TestGenerateAndEmailPullslip_EmptyBody(t *testing.T) {
	svc := newEmailSvc(&mockEmailPrRepo{}, &mockEmailService{}, nil)
	event := events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{Selector: "cql.allRecords=1", Owner: "ISIL:OWNER"},
			},
			CustomData: map[string]any{
				"to": []string{"a@b.com"}, "subject": "Sub", "body": "",
			},
		},
	}
	status, _ := svc.generateAndEmailPullslip(testCtx, event)
	assert.Equal(t, events.EventStatusError, status)
}

func TestGenerateAndEmailPullslip_ListPatronRequestsError(t *testing.T) {
	prRepo := &mockEmailPrRepo{listErr: errors.New("db down")}
	svc := newEmailSvc(prRepo, &mockEmailService{}, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
}

func TestGenerateAndEmailPullslip_SMTPError(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
	mailer := &mockEmailService{err: errors.New("SMTP unavailable")}
	svc := newEmailSvc(prRepo, mailer, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.True(t, mailer.called)
}

func TestGenerateAndEmailPullslip_Success(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
	mailer := &mockEmailService{}
	svc := newEmailSvc(prRepo, mailer, nil)
	status, result := svc.generateAndEmailPullslip(testCtx, validEmailEvent())
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, result)
	assert.True(t, mailer.called)

	assert.True(t, strings.Contains(string(mailer.data), "user@example.com"))
}

func TestGenerateAndEmailPullslip_WithPDF_Success(t *testing.T) {
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
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
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
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
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
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
	prRepo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
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
