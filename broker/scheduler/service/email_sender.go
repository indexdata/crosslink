package sched_service

import (
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/email"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	psservice "github.com/indexdata/crosslink/broker/pullslip/service"
	"github.com/indexdata/crosslink/broker/repo"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
)

const COMP = "email_sender"

var (
	MAX_RECORDS_PER_EMAIL = int32(utils.Must(utils.GetEnvInt("BATCH_PULLSLIP_MAX_COUNT", 100)))
)

type pullslipEmailData struct {
	To            []string `json:"to"`
	TemplateLabel string   `json:"templateLabel"`
	IncludePdf    bool     `json:"includePdf"`
	SendEmpty     bool     `json:"sendEmpty"`
}

type EmailSenderService struct {
	eventBus     events.EventBus
	prRepo       pr_db.PrRepo
	illRepo      ill_db.IllRepo
	pdf          psservice.PdfService
	emailService email.EmailService
	deliveryRepo sched_db.BatchEmailDeliveryRepo
}

func NewEmailSenderService(prRepo pr_db.PrRepo, illRepo ill_db.IllRepo, eventBus events.EventBus, deliveryRepo sched_db.BatchEmailDeliveryRepo) (*EmailSenderService, error) {
	emailService := email.NewEmailService()
	var err error
	if !emailService.IsReadyToSend() {
		err = errors.New("email: SMTP_HOST environment variable is required")
	}
	return &EmailSenderService{
		eventBus:     eventBus,
		prRepo:       prRepo,
		illRepo:      illRepo,
		pdf:          psservice.NewPdfService(prRepo),
		emailService: emailService,
		deliveryRepo: deliveryRepo,
	}, err
}

// EmailSenderServiceWithClient constructs an EmailSenderService with injected
// dependencies, intended for use in tests.
func EmailSenderServiceWithClient(prRepo pr_db.PrRepo, illRepo ill_db.IllRepo, emailService email.EmailService, pdf psservice.PdfService, eventBus events.EventBus, deliveryRepo sched_db.BatchEmailDeliveryRepo) *EmailSenderService {
	return &EmailSenderService{eventBus: eventBus, prRepo: prRepo, illRepo: illRepo, pdf: pdf, emailService: emailService, deliveryRepo: deliveryRepo}
}

func (s *EmailSenderService) EmailPullslip(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	if repo.BatchAttemptFrom(ctx).ID != "" {
		delivery, err := s.deliveryRepo.GetBatchEmailDelivery(ctx, event.ID)
		if err == nil {
			return s.resumeEmailPullslip(ctx, event, delivery)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return events.NewErrorResult("failed to read email delivery progress", err.Error())
		}
	}
	if s.emailService.IsReadyToSend() {
		return s.generateAndEmailPullslip(ctx, event)
	}
	return s.emailPullslipMarkFailed(ctx, event)
}

func (s *EmailSenderService) resumeEmailPullslip(ctx common.ExtendedContext, event events.Event, delivery events.BatchActionEmailDelivery) (events.EventStatus, *events.EventResult) {
	if !delivery.AcceptedAt.Valid {
		return events.NewErrorResult("email delivery needs investigation", "SMTP acceptance is unconfirmed")
	}
	if !delivery.QueuePrintedActions {
		return events.EventStatusSuccess, nil
	}
	batch := event.EventData.BatchActionData
	if batch == nil || batch.Owner == "" {
		return events.NewErrorResult("cannot resume printed actions", "batch owner is required")
	}
	requests := make([]pr_db.PatronRequest, 0, len(delivery.SelectedRequestIds))
	for _, id := range delivery.SelectedRequestIds {
		request, err := s.prRepo.GetPatronRequestById(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return events.NewErrorResult("failed to read selected patron request", err.Error())
		}
		if request.SupplierSymbol.Valid && request.SupplierSymbol.String == batch.Owner {
			requests = append(requests, request)
		}
	}
	return s.queuePrintedActions(ctx, event, requests)
}

func (s *EmailSenderService) queuePrintedActions(ctx common.ExtendedContext, event events.Event, requests []pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	if err := psservice.QueuePullslipPrinted(events.WithWorkerContext(s.eventBus, ctx), requests, &event.ID, event.EventData.BatchActionData); err != nil {
		return events.NewErrorResult("email sent but failed to queue pullslip-printed actions", err.Error())
	}
	return events.EventStatusSuccess, nil
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
	if emailData.TemplateLabel == "" {
		return events.NewErrorResult("invalid email event data", "templateLabel field is required")
	}

	template, err := prservice.ResolveTemplate(ctx, s.prRepo, pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams{
		Owner:    event.EventData.BatchActionData.Owner,
		Purpose:  string(proapi.Email),
		Label:    emailData.TemplateLabel,
		Audience: string(proapi.ModelActionParamsSendToStaff),
	})
	if err != nil {
		return events.NewErrorResult("failed to load email template", err.Error())
	}
	if !template.Subject.Valid || template.Subject.String == "" {
		return events.NewErrorResult("invalid email template", "subject field is required")
	}
	if template.Body == "" {
		return events.NewErrorResult("invalid email template", "body field is required")
	}
	if err := prservice.ValidateTemplateRenderingForContext(
		proapi.Email,
		proapi.TemplateContentType(template.ContentType),
		template.Body,
		&template.Subject.String,
		template.Labels,
		prservice.BatchEmailTemplateContext,
	); err != nil {
		return events.NewErrorResult("invalid email template", err.Error())
	}

	prs, fullCount, err := s.prRepo.ListPatronRequests(ctx, pr_db.ListPatronRequestsParams{Limit: MAX_RECORDS_PER_EMAIL, Offset: 0}, pgcql)
	if err != nil {
		return events.NewErrorResult("did not select data for processing", err.Error())
	}
	if fullCount > int64(MAX_RECORDS_PER_EMAIL) {
		ctx.Logger().Warn("email batch truncated: selector matched more records than the per-email limit",
			"matched", fullCount, "limit", MAX_RECORDS_PER_EMAIL)
	}
	if len(prs) == 0 && !emailData.SendEmpty {
		return events.EventStatusSuccess, &events.EventResult{CommonEventData: events.CommonEventData{Note: "no patron requests matched the selector"}}
	}

	// Optionally generate a pull-slip PDF and attach it.
	var pdfAttachment *email.PdfAttach
	includePdf := emailData.IncludePdf && len(prs) > 0
	if includePdf {
		if s.pdf == nil {
			return events.NewErrorResult("pdf not configured", "no PDF generator is available on this service instance")
		}
		pdfBytes, pdfErr := s.pdf.GeneratePdfPullSlipForPrs(ctx, prs)
		if pdfErr != nil {
			return events.NewErrorResult("failed to generate pdf file", pdfErr.Error())
		}
		pdfAttachment = &email.PdfAttach{Filename: "pull-slips.pdf", Data: pdfBytes}
	}

	placeholders := email.GetBatchEmailData(fullCount, len(prs), event.EventData.BatchActionData.Selector)
	var body string
	if template.ContentType == string(proapi.Html) {
		body, err = email.RenderHtmlTemplate(placeholders, template.Labels, template.Body)
	} else {
		body, err = email.RenderTextTemplate(placeholders, template.Labels, template.Body)
	}
	if err != nil {
		return events.NewErrorResult("failed to render email body", err.Error())
	}
	subject, err := email.RenderTextTemplate(placeholders, template.Labels, template.Subject.String)
	if err != nil {
		return events.NewErrorResult("failed to render email subject", err.Error())
	}
	messageData := email.EmailData{
		To:         emailData.To,
		Subject:    subject,
		Body:       body,
		IsHTML:     template.ContentType == string(proapi.Html),
		IncludePdf: includePdf,
	}

	raw, err := email.BuildRawMessage(*owner.CustomData.FromEmail, messageData, pdfAttachment)
	if err != nil {
		return events.NewErrorResult("failed to build email message", err.Error())
	}

	requestIDs := make([]string, 0, len(prs))
	for _, pr := range prs {
		requestIDs = append(requestIDs, pr.ID)
	}
	if err := s.deliveryRepo.RecordBatchEmailIntent(ctx, event.ID, requestIDs, includePdf); err != nil {
		return events.NewErrorResult("failed to record email delivery intent", err.Error())
	}
	err = s.emailService.SendEmail(*owner.CustomData.FromEmail, messageData.To, raw)
	if err != nil {
		return events.NewErrorResult("failed to send email via SMTP", err.Error())
	}
	if err := s.recordEmailAcceptance(ctx, event.ID); err != nil {
		return events.NewErrorResult("SMTP accepted email but failed to record acceptance", err.Error())
	}
	if includePdf {
		return s.queuePrintedActions(ctx, event, prs)
	}
	return events.EventStatusSuccess, nil
}

// extractEmailData retrieves email pullslip parameters from the event's CustomData map.
func extractEmailData(eventData events.EventData) (pullslipEmailData, error) {
	if eventData.CustomData == nil {
		return pullslipEmailData{}, fmt.Errorf("customData is nil")
	}

	toRaw, ok := eventData.CustomData["to"]
	if !ok {
		return pullslipEmailData{}, fmt.Errorf("missing 'to' field in customData")
	}
	var toAddrs []string
	switch v := toRaw.(type) {
	case []string:
		toAddrs = v
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return pullslipEmailData{}, fmt.Errorf("'to' field contains non-string value")
			}
			toAddrs = append(toAddrs, s)
		}
	default:
		return pullslipEmailData{}, fmt.Errorf("'to' field has unexpected type %T", toRaw)
	}

	templateLabel, _ := eventData.CustomData["templateLabel"].(string)
	includePdf, _ := eventData.CustomData["includePdf"].(bool)
	sendEmpty, _ := eventData.CustomData["sendEmpty"].(bool)

	return pullslipEmailData{
		To:            toAddrs,
		TemplateLabel: templateLabel,
		IncludePdf:    includePdf,
		SendEmpty:     sendEmpty,
	}, nil
}
