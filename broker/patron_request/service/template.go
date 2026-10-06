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

func GetStateModelTemplateDefault(purpose proapi.TemplatePurpose, audience proapi.TemplateAudience, label string) (pr_db.Template, error) {
	var selected *proapi.CreateTemplate
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
