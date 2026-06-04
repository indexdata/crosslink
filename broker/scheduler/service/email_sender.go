package sched_service

import (
	"bytes"

	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/smtp"
	"net/textproto"
	"strings"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	psservice "github.com/indexdata/crosslink/broker/pullslip/service"
	"github.com/indexdata/go-utils/utils"
)

const COMP = "email_sender"

// Environment variables for SMTP configuration.
var (
	SMTP_HOST             = utils.GetEnv("SMTP_HOST", "")
	SMTP_PORT             = utils.GetEnv("SMTP_PORT", "2525")
	SMTP_USERNAME         = utils.GetEnv("SMTP_USERNAME", "")
	SMTP_PASSWORD         = utils.GetEnv("SMTP_PASSWORD", "")
	MAX_RECORDS_PER_EMAIL = int32(utils.Must(utils.GetEnvInt("BATCH_PULLSLIP_MAX_COUNT", 100)))
)

// Mailer is an interface over smtp.SendMail, allowing mocking in tests.
type Mailer interface {
	SendMail(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

type DefaultMailer struct{}

func (m *DefaultMailer) SendMail(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, a, from, to, msg)
}

// EmailData carries the email payload inside an EventData.CustomData map.
type EmailData struct {
	To         []string `json:"to"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
	IsHTML     bool     `json:"isHtml,omitempty"`
	IncludePdf bool     `json:"includePdf,omitempty"`
}

type EmailSenderService struct {
	prRepo      pr_db.PrRepo
	illRepo     ill_db.IllRepo
	pdf         psservice.PdfService
	mailer      Mailer
	smtpAddr    string
	smtpAuth    smtp.Auth
	readyToSend bool
}

func NewEmailSenderService(prRepo pr_db.PrRepo, illRepo ill_db.IllRepo) (*EmailSenderService, error) {
	if SMTP_HOST == "" {
		return &EmailSenderService{
			prRepo:      prRepo,
			illRepo:     illRepo,
			readyToSend: false,
		}, errors.New("email: SMTP_HOST environment variable is required")
	}

	var auth smtp.Auth
	if SMTP_USERNAME != "" {
		auth = smtp.PlainAuth("", SMTP_USERNAME, SMTP_PASSWORD, SMTP_HOST)
	}

	pdfSvc := psservice.NewPdfService(prRepo)

	return &EmailSenderService{
		prRepo:      prRepo,
		illRepo:     illRepo,
		mailer:      &DefaultMailer{},
		smtpAddr:    fmt.Sprintf("%s:%s", SMTP_HOST, SMTP_PORT),
		smtpAuth:    auth,
		pdf:         pdfSvc,
		readyToSend: true,
	}, nil
}

// EmailSenderServiceWithClient constructs an EmailSenderService with injected
// dependencies, intended for use in tests.
func EmailSenderServiceWithClient(prRepo pr_db.PrRepo, illRepo ill_db.IllRepo, mailer Mailer, pdf psservice.PdfService, readyToSend bool) *EmailSenderService {
	return &EmailSenderService{prRepo: prRepo, illRepo: illRepo, mailer: mailer, pdf: pdf, readyToSend: readyToSend}
}

func (s *EmailSenderService) EmailPullslip(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	if s.readyToSend {
		return s.generateAndEmailPullslip(ctx, event)
	}
	return s.emailPullslipMarkFailed(ctx, event)
}

func (s *EmailSenderService) emailPullslipMarkFailed(_ common.ExtendedContext, _ events.Event) (events.EventStatus, *events.EventResult) {
	return events.NewErrorResult("email not sent", "email sending configuration missing")
}

func (s *EmailSenderService) generateAndEmailPullslip(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	if event.EventData.BatchActionData == nil || event.EventData.BatchActionData.Selector == "" ||
		event.EventData.BatchActionData.Owner == "" {
		return events.NewErrorResult("invalid email event data", "batch action data is empty")
	}
	pgcql, err := pr_db.ParsePatronRequestsCql(event.EventData.BatchActionData.Selector)
	if err != nil {
		return events.NewErrorResult("invalid cql selector", err.Error())
	}

	owner, err := s.illRepo.GetPeerBySymbol(ctx, event.EventData.BatchActionData.Owner)
	if err != nil {
		return events.NewErrorResult("invalid email event data", "owner not found: "+err.Error())
	}

	if owner.CustomData.FromEmail == nil || *owner.CustomData.FromEmail == "" {
		return events.NewErrorResult("invalid email event data", "owner is missing fromEmail in customData")
	}

	emailData, err := extractEmailData(event.EventData)
	if err != nil {
		return events.NewErrorResult("invalid email event data", err.Error())
	}
	if len(emailData.To) == 0 {
		return events.NewErrorResult("invalid email event data", "to field is required")
	}
	if emailData.Subject == "" {
		return events.NewErrorResult("invalid email event data", "subject field is required")
	}
	if emailData.Body == "" {
		return events.NewErrorResult("invalid email event data", "body field is required")
	}

	prs, fullCount, err := s.prRepo.ListPatronRequests(ctx, pr_db.ListPatronRequestsParams{Limit: MAX_RECORDS_PER_EMAIL, Offset: 0}, pgcql)
	if err != nil {
		return events.NewErrorResult("did not select data for processing", err.Error())
	}
	if fullCount > int64(MAX_RECORDS_PER_EMAIL) {
		ctx.Logger().Warn("email batch truncated: selector matched more records than the per-email limit",
			"matched", fullCount, "limit", MAX_RECORDS_PER_EMAIL)
	}

	// Optionally generate a pull-slip PDF and attach it.
	var pdfAttachment *pdfAttach
	if emailData.IncludePdf {
		if s.pdf == nil {
			return events.NewErrorResult("pdf not configured", "no PDF generator is available on this service instance")
		}
		pdfBytes, pdfErr := s.pdf.GeneratePdfPullSlipForPrs(ctx, prs)
		if pdfErr != nil {
			return events.NewErrorResult("failed to generate pdf file", pdfErr.Error())
		}
		pdfAttachment = &pdfAttach{filename: "pull-slips.pdf", data: pdfBytes}
	}

	emailData.Body = strings.ReplaceAll(emailData.Body, "{{fullCount}}", fmt.Sprintf("%d", fullCount))
	emailData.Body = strings.ReplaceAll(emailData.Body, "{{actualCount}}", fmt.Sprintf("%d", len(prs)))
	emailData.Body = strings.ReplaceAll(emailData.Body, "{{batchQuery}}", event.EventData.BatchActionData.Selector)

	raw, err := buildRawMessage(*owner.CustomData.FromEmail, emailData, pdfAttachment)
	if err != nil {
		return events.NewErrorResult("failed to build email message", err.Error())
	}

	err = s.mailer.SendMail(s.smtpAddr, s.smtpAuth, *owner.CustomData.FromEmail, emailData.To, raw)
	if err != nil {
		return events.NewErrorResult("failed to send email via SMTP", err.Error())
	}
	return events.EventStatusSuccess, nil
}

// pdfAttach holds a PDF file to attach to the email.
type pdfAttach struct {
	filename string
	data     []byte
}

// buildRawMessage constructs a MIME multipart/mixed raw message.
// If attachment is non-nil its bytes are included as a PDF attachment.
func buildRawMessage(fromAddr string, data EmailData, attachment *pdfAttach) ([]byte, error) {
	if strings.ContainsAny(fromAddr, "\r\n") {
		return nil, errors.New("header injection detected in fromAddr")
	}
	if strings.ContainsAny(data.Subject, "\r\n") {
		return nil, errors.New("header injection detected in subject")
	}
	for _, addr := range data.To {
		if strings.ContainsAny(addr, "\r\n") {
			return nil, errors.New("header injection detected in to address")
		}
	}

	var buf bytes.Buffer

	// Create the multipart writer first to capture its randomly-generated
	// boundary, then reset the buffer so the top-level headers are written
	// before the first MIME part.
	mw := multipart.NewWriter(&buf)
	buf.Reset()
	buf.WriteString("From: " + fromAddr + "\r\n")
	buf.WriteString("To: " + joinAddresses(data.To) + "\r\n")
	buf.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", data.Subject) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n\r\n")

	// Body part.
	bodyHeaders := make(textproto.MIMEHeader)
	if data.IsHTML {
		bodyHeaders.Set("Content-Type", "text/html; charset=UTF-8")
	} else {
		bodyHeaders.Set("Content-Type", "text/plain; charset=UTF-8")
	}
	bodyHeaders.Set("Content-Transfer-Encoding", "quoted-printable")

	bodyPart, err := mw.CreatePart(bodyHeaders)
	if err != nil {
		return nil, fmt.Errorf("create body part: %w", err)
	}
	qpw := quotedprintable.NewWriter(bodyPart)
	if _, err = qpw.Write([]byte(data.Body)); err != nil {
		return nil, fmt.Errorf("write body: %w", err)
	}
	if err = qpw.Close(); err != nil {
		return nil, fmt.Errorf("close qp writer: %w", err)
	}

	// PDF attachment part.
	if attachment != nil {
		attHeaders := make(textproto.MIMEHeader)
		attHeaders.Set("Content-Type", `application/pdf; name="`+attachment.filename+`"`)
		attHeaders.Set("Content-Transfer-Encoding", "base64")
		attHeaders.Set("Content-Disposition", `attachment; filename="`+attachment.filename+`"`)

		attPart, createErr := mw.CreatePart(attHeaders)
		if createErr != nil {
			return nil, fmt.Errorf("create attachment part: %w", createErr)
		}
		// Encode as base64 with RFC 2045 line wrapping (76 chars + CRLF).
		enc := base64.StdEncoding.EncodeToString(attachment.data)
		for i := 0; i < len(enc); i += 76 {
			end := i + 76
			if end > len(enc) {
				end = len(enc)
			}
			if _, writeErr := attPart.Write([]byte(enc[i:end] + "\r\n")); writeErr != nil {
				return nil, fmt.Errorf("write attachment: %w", writeErr)
			}
		}
	}

	if err = mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}
	return buf.Bytes(), nil
}

// joinAddresses joins email addresses with ", ".
func joinAddresses(addrs []string) string {
	result := ""
	for i, a := range addrs {
		if i > 0 {
			result += ", "
		}
		result += a
	}
	return result
}

// extractEmailData retrieves EmailData from the event's CustomData map.
func extractEmailData(eventData events.EventData) (EmailData, error) {
	if eventData.CustomData == nil {
		return EmailData{}, fmt.Errorf("customData is nil")
	}

	toRaw, ok := eventData.CustomData["to"]
	if !ok {
		return EmailData{}, fmt.Errorf("missing 'to' field in customData")
	}
	var toAddrs []string
	switch v := toRaw.(type) {
	case []string:
		toAddrs = v
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return EmailData{}, fmt.Errorf("'to' field contains non-string value")
			}
			toAddrs = append(toAddrs, s)
		}
	default:
		return EmailData{}, fmt.Errorf("'to' field has unexpected type %T", toRaw)
	}

	subject, _ := eventData.CustomData["subject"].(string)
	body, _ := eventData.CustomData["body"].(string)
	isHTML, _ := eventData.CustomData["isHtml"].(bool)
	includePdf, _ := eventData.CustomData["includePdf"].(bool)

	return EmailData{
		To:         toAddrs,
		Subject:    subject,
		Body:       body,
		IsHTML:     isHTML,
		IncludePdf: includePdf,
	}, nil
}
