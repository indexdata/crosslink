package prservice

import (
	"testing"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTemplateSamplesLoadsEverySampleInOrder(t *testing.T) {
	samples, err := GetTemplateSamples()
	require.NoError(t, err)
	require.Len(t, samples, len(TemplateSampleNames))
	for i, sample := range samples {
		assert.Equal(t, TemplateSampleNames[i], sample.Name)
		assert.Equal(t, "sample-"+sample.Name, sample.Request.ID)
		assert.NotEmpty(t, sample.Request.RequesterReqID.String, sample.Name)
		for _, note := range sample.Notes {
			assert.Equal(t, pr_db.NotificationKindNote, note.Kind)
			assert.True(t, note.Note.Valid)
		}
		for _, condition := range sample.Conditions {
			assert.Equal(t, pr_db.NotificationKindCondition, condition.Kind)
			assert.True(t, condition.Condition.Valid)
		}
		want := SideLending
		if sample.Name == "requester" {
			want = SideBorrowing
		}
		assert.Equal(t, want, sample.Request.Side, sample.Name)
	}
}

func TestTemplateSampleCopiesAreIndependent(t *testing.T) {
	first, err := GetTemplateSamples()
	require.NoError(t, err)
	first[0].Request.IllRequest.BibliographicInfo.Title = "changed"
	first[0].Request.Items[0].Barcode = "changed"
	second, err := GetTemplateSamples()
	require.NoError(t, err)
	assert.Equal(t, "The Art of Computer Programming", second[0].Request.IllRequest.BibliographicInfo.Title)
	assert.Equal(t, "31234000567890", second[0].Request.Items[0].Barcode)
}
