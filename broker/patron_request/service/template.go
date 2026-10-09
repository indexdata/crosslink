package prservice

import (
	"errors"
	"fmt"
	"slices"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/email"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TemplateRenderingContext identifies the data shape available to a template.
type TemplateRenderingContext int

const (
	// PatronRequestTemplateContext validates templates rendered for one patron request.
	PatronRequestTemplateContext TemplateRenderingContext = iota
	// BatchEmailTemplateContext validates templates rendered for scheduled email batches.
	BatchEmailTemplateContext
)

func ValidateTemplateRendering(purpose proapi.TemplatePurpose, contentType proapi.TemplateContentType, body string, subject *string, labels []string) error {
	if purpose == proapi.Pullslip {
		return ValidateTemplateRenderingForContext(purpose, contentType, body, nil, labels, PatronRequestTemplateContext)
	}

	if slices.Contains(labels, "pullslip-email") {
		for _, label := range labels {
			if label != "pullslip-email" {
				return errors.New("batch email label pullslip-email cannot combine with patron-request notification labels")
			}
		}
		if err := ValidateTemplateRenderingForContext(purpose, contentType, body, subject, labels, BatchEmailTemplateContext); err != nil {
			return fmt.Errorf("template does not render with batch-email data: %w", err)
		}
		return nil
	}

	patronErr := ValidateTemplateRenderingForContext(purpose, contentType, body, subject, labels, PatronRequestTemplateContext)
	if patronErr == nil {
		return nil
	}
	batchErr := ValidateTemplateRenderingForContext(purpose, contentType, body, subject, labels, BatchEmailTemplateContext)
	if batchErr == nil {
		return nil
	}
	return fmt.Errorf("template does not render with patron-request or batch-email data: patron-request: %w; batch-email: %v", patronErr, batchErr)
}

// ValidateTemplateRenderingForContext validates a template against its explicit rendering data context.
func ValidateTemplateRenderingForContext(purpose proapi.TemplatePurpose, contentType proapi.TemplateContentType, body string, subject *string, labels []string, context TemplateRenderingContext) error {
	if purpose == proapi.Pullslip && context != PatronRequestTemplateContext {
		return errors.New("pullslip templates require patron-request rendering context")
	}
	if purpose == proapi.Pullslip && contentType != proapi.Html {
		return errors.New("pullslip templates must use HTML content type")
	}

	var data any
	switch context {
	case PatronRequestTemplateContext:
		data = email.GetPullSlipData(pr_db.PatronRequest{}, nil, nil, email.DEFAULT_FOR_NO_VALUE)
		// Request IDs can be empty in a zero-valued request, but runtime
		// rendering supplies a populated request ID.
		patronData := data.(email.PullSlipData)
		patronData.ReqId = email.DEFAULT_FOR_NO_VALUE
		data = patronData
	case BatchEmailTemplateContext:
		data = email.GetBatchEmailData(1, 1, email.DEFAULT_FOR_NO_VALUE)
	default:
		return fmt.Errorf("unknown template rendering context %d", context)
	}

	var err error
	if contentType == proapi.Html {
		err = email.ValidateHtmlTemplate(data, labels, body)
	} else {
		err = email.ValidateTextTemplate(data, labels, body)
	}
	if err != nil {
		return fmt.Errorf("template body: %w", err)
	}
	if subject != nil {
		if err := email.ValidateTextTemplate(data, labels, *subject); err != nil {
			return fmt.Errorf("template subject: %w", err)
		}
	}
	return nil
}

func GetStateModelTemplateDefault(purpose proapi.TemplatePurpose, audience proapi.TemplateAudience, label string) (pr_db.Template, error) {
	var selected *proapi.TemplateProperties
	for i := range stateModelsConfig.TemplateDefaults {
		template := &stateModelsConfig.TemplateDefaults[i]
		if template.Purpose != purpose || !slices.Contains(template.Labels, label) {
			continue
		}
		if template.Audience == nil {
			if selected == nil {
				selected = template
			}
			continue
		}
		if *template.Audience == audience {
			selected = template
			break
		}
	}
	if selected == nil {
		return pr_db.Template{}, fmt.Errorf("no state-model default template found for purpose %q, audience %q, and label %q", purpose, audience, label)
	}

	subject := pgtype.Text{}
	if selected.Subject != nil {
		subject = pgtype.Text{String: *selected.Subject, Valid: true}
	}
	templateAudience := pgtype.Text{}
	if selected.Audience != nil {
		templateAudience = pgtype.Text{String: string(*selected.Audience), Valid: true}
	}
	return pr_db.Template{
		Title:       selected.Title,
		Purpose:     string(selected.Purpose),
		Subject:     subject,
		Body:        selected.Body,
		ContentType: string(selected.ContentType),
		Labels:      slices.Clone(selected.Labels),
		Audience:    templateAudience,
	}, nil
}

func ResolveTemplate(ctx common.ExtendedContext, repo pr_db.PrRepo, params pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams) (pr_db.Template, error) {
	template, err := repo.GetTemplateByPurposeAudienceLabelAndOwner(ctx, params)
	if err == nil {
		return template, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pr_db.Template{}, err
	}
	return GetStateModelTemplateDefault(
		proapi.TemplatePurpose(params.Purpose),
		proapi.TemplateAudience(params.Audience),
		params.Label,
	)
}
