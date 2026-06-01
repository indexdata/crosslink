package sched_service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	psservice "github.com/indexdata/crosslink/broker/pullslip/service"
	"github.com/indexdata/go-utils/utils"
)

const COMP = "email_sender"

// maxRecordsPerEmail caps the number of patron requests fetched for a single
// email batch. Selectors matching more records will be truncated with a warning.
const maxRecordsPerEmail = 100

// Environment variables for SES configuration.
var (
	SES_REGION    = utils.GetEnv("SES_REGION", "")
	SES_FROM_ADDR = utils.GetEnv("SES_FROM_ADDR", "")
)

// SESClient is an interface over ses.SendRawEmail, allowing mocking in tests.
// SendRawEmail is required (vs SendEmail) because it supports attachments.
type SESClient interface {
	SendRawEmail(ctx context.Context, params *ses.SendRawEmailInput, optFns ...func(*ses.Options)) (*ses.SendRawEmailOutput, error)
}

// PdfGenerator generates a merged PDF pull-slip for a list of patron requests.
type PdfGenerator interface {
	GeneratePdfPullSlipForPrs(ctx common.ExtendedContext, prs []pr_db.PatronRequest) ([]byte, error)
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
	eventBus    events.EventBus
	pdf         PdfGenerator
	client      SESClient
	fromAddr    string
	readyToSend bool
}

func NewEmailSenderService(prRepo pr_db.PrRepo, eventBus events.EventBus) (*EmailSenderService, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if SES_REGION != "" {
		opts = append(opts, awsconfig.WithRegion(SES_REGION))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return &EmailSenderService{
			prRepo:      prRepo,
			eventBus:    eventBus,
			readyToSend: false,
		}, fmt.Errorf("email: failed to load AWS config: %w", err)
	}
	if SES_FROM_ADDR == "" {
		return &EmailSenderService{
			prRepo:      prRepo,
			eventBus:    eventBus,
			readyToSend: false,
		}, fmt.Errorf("email: SES_FROM_ADDR environment variable is required")
	}
	pdfSvc := psservice.NewPdfService(prRepo)
	sesEndpointOverride := utils.GetEnv("SES_ENDPOINT_OVERRIDE", "")
	sesClient := ses.NewFromConfig(cfg, func(o *ses.Options) {
		if sesEndpointOverride != "" {
			o.BaseEndpoint = aws.String(sesEndpointOverride)
		}
	})
	return &EmailSenderService{
		prRepo:      prRepo,
		eventBus:    eventBus,
		client:      sesClient,
		fromAddr:    SES_FROM_ADDR,
		pdf:         pdfSvc,
		readyToSend: true,
	}, nil
}

// EmailSenderServiceWithClient constructs an EmailSenderService with injected
// dependencies, intended for use in tests.
func EmailSenderServiceWithClient(prRepo pr_db.PrRepo, eventBus events.EventBus, client SESClient, fromAddr string, pdf PdfGenerator) *EmailSenderService {
	return &EmailSenderService{prRepo: prRepo, eventBus: eventBus, client: client, fromAddr: fromAddr, pdf: pdf}
}

func (s *EmailSenderService) EmailPullslip(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	if s.readyToSend {
		_, _ = s.eventBus.ProcessTask(ctx, event, events.SignalConsumers, s.generateAndEmailPullslip)
	} else {
		_, _ = s.eventBus.ProcessTask(ctx, event, events.SignalConsumers, s.emailPullslipMarkFailed)
	}
}

func (s *EmailSenderService) emailPullslipMarkFailed(_ common.ExtendedContext, _ events.Event) (events.EventStatus, *events.EventResult) {
	return events.NewErrorResult("email not sent", "email sending configuration missing")
}

func (s *EmailSenderService) generateAndEmailPullslip(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	if event.EventData.BatchActionData == nil || event.EventData.BatchActionData.Selector == "" {
		return events.NewErrorResult("invalid email event data", "batch action data is empty")
	}
	pgcql, err := pr_db.ParsePatronRequestsCql(event.EventData.BatchActionData.Selector)
	if err != nil {
		return events.NewErrorResult("invalid cql selector", err.Error())
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

	prs, fullCount, err := s.prRepo.ListPatronRequests(ctx, pr_db.ListPatronRequestsParams{Limit: maxRecordsPerEmail, Offset: 0}, pgcql)
	if err != nil {
		return events.NewErrorResult("did not select data for processing", err.Error())
	}
	if fullCount > maxRecordsPerEmail {
		ctx.Logger().Warn("email batch truncated: selector matched more records than the per-email limit",
			"matched", fullCount, "limit", maxRecordsPerEmail)
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

	raw, err := buildRawMessage(s.fromAddr, emailData, pdfAttachment)
	if err != nil {
		return events.NewErrorResult("failed to build email message", err.Error())
	}

	_, err = s.client.SendRawEmail(ctx, &ses.SendRawEmailInput{
		RawMessage: &types.RawMessage{Data: raw},
	})
	if err != nil {
		return events.NewErrorResult("failed to send email via SES", err.Error())
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
		// Encode as base64
		encoder := base64.NewEncoder(base64.StdEncoding, attPart)
		if _, writeErr := encoder.Write(attachment.data); writeErr != nil {
			return nil, fmt.Errorf("write attachment: %w", writeErr)
		}
		if closeErr := encoder.Close(); closeErr != nil {
			return nil, fmt.Errorf("close attachment encoder: %w", closeErr)
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
