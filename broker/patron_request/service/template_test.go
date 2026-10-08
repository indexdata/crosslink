package prservice

import (
	"testing"

	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/stretchr/testify/assert"
)

func TestGetStateModelTemplateDefaultMatchesPurposeAudienceAndLabel(t *testing.T) {
	template, err := GetStateModelTemplateDefault(
		proapi.Email,
		proapi.TemplateAudiencePatron,
		"copy-completed-notification",
	)

	assert.NoError(t, err)
	assert.Equal(t, "Your requested document is ready", template.Subject.String)
	assert.Equal(t, "text", template.ContentType)

	_, err = GetStateModelTemplateDefault(
		proapi.Pullslip,
		proapi.TemplateAudiencePatron,
		"copy-completed-notification",
	)
	assert.ErrorContains(t, err, "no state-model default template")

	_, err = GetStateModelTemplateDefault(
		proapi.Email,
		proapi.TemplateAudienceStaff,
		"copy-completed-notification",
	)
	assert.ErrorContains(t, err, "no state-model default template")
}

func TestGetStateModelTemplateDefaultAudienceNeutralMatchesAnyAudience(t *testing.T) {
	originalDefaults := stateModelsConfig.TemplateDefaults
	t.Cleanup(func() {
		stateModelsConfig.TemplateDefaults = originalDefaults
	})
	stateModelsConfig.TemplateDefaults = append(
		append([]proapi.TemplateProperties(nil), originalDefaults...),
		proapi.TemplateProperties{
			Title:       "Audience-neutral notification",
			Purpose:     proapi.Email,
			Body:        "Notification body",
			ContentType: proapi.Text,
			Labels:      []string{"audience-neutral-notification"},
		},
	)

	for _, audience := range []proapi.TemplateAudience{
		proapi.TemplateAudiencePatron,
		proapi.TemplateAudienceStaff,
	} {
		t.Run(string(audience), func(t *testing.T) {
			template, err := GetStateModelTemplateDefault(
				proapi.Email,
				audience,
				"audience-neutral-notification",
			)

			assert.NoError(t, err)
			assert.Equal(t, "Notification body", template.Body)
			assert.False(t, template.Audience.Valid)
		})
	}
}

func TestGetStateModelTemplateDefaultPrefersExactAudienceOverNeutral(t *testing.T) {
	originalDefaults := stateModelsConfig.TemplateDefaults
	t.Cleanup(func() {
		stateModelsConfig.TemplateDefaults = originalDefaults
	})
	patronAudience := proapi.TemplateAudiencePatron
	stateModelsConfig.TemplateDefaults = []proapi.TemplateProperties{
		{
			Title:       "Audience-neutral notification",
			Purpose:     proapi.Email,
			Body:        "Neutral notification body",
			ContentType: proapi.Text,
			Labels:      []string{"overlapping-audience-notification"},
		},
		{
			Title:       "Patron notification",
			Purpose:     proapi.Email,
			Body:        "Patron notification body",
			ContentType: proapi.Text,
			Labels:      []string{"overlapping-audience-notification"},
			Audience:    &patronAudience,
		},
	}

	patronTemplate, err := GetStateModelTemplateDefault(
		proapi.Email,
		proapi.TemplateAudiencePatron,
		"overlapping-audience-notification",
	)
	assert.NoError(t, err)
	assert.Equal(t, "Patron notification body", patronTemplate.Body)
	assert.Equal(t, string(proapi.TemplateAudiencePatron), patronTemplate.Audience.String)
	assert.True(t, patronTemplate.Audience.Valid)

	staffTemplate, err := GetStateModelTemplateDefault(
		proapi.Email,
		proapi.TemplateAudienceStaff,
		"overlapping-audience-notification",
	)
	assert.NoError(t, err)
	assert.Equal(t, "Neutral notification body", staffTemplate.Body)
	assert.False(t, staffTemplate.Audience.Valid)
}
