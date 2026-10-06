package prservice

import (
	"errors"
	"fmt"
	"slices"

	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// GetStateModelTemplateDefault returns the configured default matching all
// template selection dimensions used by database-backed template lookups.
func GetStateModelTemplateDefault(purpose proapi.TemplatePurpose, audience proapi.TemplateAudience, label string) (pr_db.Template, error) {
	for _, template := range stateModelsConfig.TemplateDefaults {
		if template.Purpose != purpose || (template.Audience != nil && *template.Audience != audience) || !slices.Contains(template.Labels, label) {
			continue
		}
		subject := pgtype.Text{}
		if template.Subject != nil {
			subject = pgtype.Text{String: *template.Subject, Valid: true}
		}
		templateAudience := pgtype.Text{}
		if template.Audience != nil {
			templateAudience = pgtype.Text{String: string(*template.Audience), Valid: true}
		}
		return pr_db.Template{
			Title:       template.Title,
			Purpose:     string(template.Purpose),
			Subject:     subject,
			Body:        template.Body,
			ContentType: string(template.ContentType),
			Labels:      slices.Clone(template.Labels),
			Audience:    templateAudience,
		}, nil
	}
	return pr_db.Template{}, fmt.Errorf("no state-model default template found for purpose %q, audience %q, and label %q", purpose, audience, label)
}

// ResolveTemplate returns an owner-specific database template, falling back to
// the embedded state-model default only when no database row exists.
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
