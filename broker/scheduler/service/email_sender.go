package sched_service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/email"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	prapi "github.com/indexdata/crosslink/broker/patron_request/api"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	psservice "github.com/indexdata/crosslink/broker/pullslip/service"
	"github.com/indexdata/go-utils/utils"
)

const COMP = "email_sender"

var (
	MAX_RECORDS_PER_EMAIL = int32(utils.Must(utils.GetEnvInt("BATCH_PULLSLIP_MAX_COUNT", 100)))
)

type EmailSenderService struct {
	prRepo       pr_db.PrRepo
	illRepo      ill_db.IllRepo
	pdf          psservice.PdfService
	emailService email.EmailService
}

func NewEmailSenderService(prRepo pr_db.PrRepo, illRepo ill_db.IllRepo) (*EmailSenderService, error) {
	emailService := email.NewEmailService()
	var err error
	if !emailService.IsReadyToSend() {
		err = errors.New("email: SMTP_HOST environment variable is required")
	}
	return &EmailSenderService{
		prRepo:       prRepo,
		illRepo:      illRepo,
		pdf:          psservice.NewPdfService(prRepo),
		emailService: emailService,
	}, err
}

// EmailSenderServiceWithClient constructs an EmailSenderService with injected
// dependencies, intended for use in tests.
func EmailSenderServiceWithClient(prRepo pr_db.PrRepo, illRepo ill_db.IllRepo, emailService email.EmailService, pdf psservice.PdfService) *EmailSenderService {
	return &EmailSenderService{prRepo: prRepo, illRepo: illRepo, pdf: pdf, emailService: emailService}
}

func (s *EmailSenderService) EmailPullslip(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	if s.emailService.IsReadyToSend() {
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

	qb, err := cqlbuilder.NewQueryFromString(event.EventData.BatchActionData.Selector)
	if err != nil {
		return events.NewErrorResult("invalid cql selector", err.Error())
	}

	var side pr_db.PatronRequestSide
	qb, err = prapi.AddOwnerRestriction(qb, event.EventData.BatchActionData.Owner, side)
	if err != nil {
		return events.NewErrorResult("failed to add owner restriction", err.Error())
	}
	builtCQL, err := qb.Build()
	if err != nil {
		return events.NewErrorResult("invalid cql selector", err.Error())
	}

	pgcql, err := pr_db.ParsePatronRequestsCql(builtCQL.String())
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
	var pdfAttachment *email.PdfAttach
	if emailData.IncludePdf {
		if s.pdf == nil {
			return events.NewErrorResult("pdf not configured", "no PDF generator is available on this service instance")
		}
		pdfBytes, pdfErr := s.pdf.GeneratePdfPullSlipForPrs(ctx, prs)
		if pdfErr != nil {
			return events.NewErrorResult("failed to generate pdf file", pdfErr.Error())
		}
		pdfAttachment = &email.PdfAttach{Filename: "pull-slips.pdf", Data: pdfBytes}
	}

	emailData.Body = strings.ReplaceAll(emailData.Body, "{{fullCount}}", fmt.Sprintf("%d", fullCount))
	emailData.Body = strings.ReplaceAll(emailData.Body, "{{actualCount}}", fmt.Sprintf("%d", len(prs)))
	emailData.Body = strings.ReplaceAll(emailData.Body, "{{batchQuery}}", event.EventData.BatchActionData.Selector)

	raw, err := email.BuildRawMessage(*owner.CustomData.FromEmail, emailData, pdfAttachment)
	if err != nil {
		return events.NewErrorResult("failed to build email message", err.Error())
	}

	err = s.emailService.SendEmail(*owner.CustomData.FromEmail, emailData.To, raw)
	if err != nil {
		return events.NewErrorResult("failed to send email via SMTP", err.Error())
	}
	return events.EventStatusSuccess, nil
}

// extractEmailData retrieves EmailData from the event's CustomData map.
func extractEmailData(eventData events.EventData) (email.EmailData, error) {
	if eventData.CustomData == nil {
		return email.EmailData{}, fmt.Errorf("customData is nil")
	}

	toRaw, ok := eventData.CustomData["to"]
	if !ok {
		return email.EmailData{}, fmt.Errorf("missing 'to' field in customData")
	}
	var toAddrs []string
	switch v := toRaw.(type) {
	case []string:
		toAddrs = v
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return email.EmailData{}, fmt.Errorf("'to' field contains non-string value")
			}
			toAddrs = append(toAddrs, s)
		}
	default:
		return email.EmailData{}, fmt.Errorf("'to' field has unexpected type %T", toRaw)
	}

	subject, _ := eventData.CustomData["subject"].(string)
	body, _ := eventData.CustomData["body"].(string)
	isHTML, _ := eventData.CustomData["isHtml"].(bool)
	includePdf, _ := eventData.CustomData["includePdf"].(bool)

	return email.EmailData{
		To:         toAddrs,
		Subject:    subject,
		Body:       body,
		IsHTML:     isHTML,
		IncludePdf: includePdf,
	}, nil
}
