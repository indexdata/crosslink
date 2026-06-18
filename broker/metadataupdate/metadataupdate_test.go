package metadataupdate

import (
	"testing"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestApplyBibliographicUpdateReplace(t *testing.T) {
	original := iso18626.BibliographicInfo{
		SupplierUniqueRecordId: "old",
		BibliographicRecordId: []iso18626.BibliographicRecordId{
			{
				BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{Text: "location"},
				BibliographicRecordIdentifier:     "Old",
			},
			{
				BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{Text: "legacy"},
				BibliographicRecordIdentifier:     "Keep",
			},
		},
	}
	updated := ApplyBibliographicUpdate(original, MetadataFields{
		LocalIdentifier:  "new",
		Location:         "Main",
		ShelvingLocation: "Stacks",
		CallNumber:       "123",
		ItemId:           "it1",
	}, directory.Replace)

	assert.Equal(t, "new", updated.SupplierUniqueRecordId)
	assert.Len(t, updated.BibliographicRecordId, 5)
	assert.Equal(t, "legacy", updated.BibliographicRecordId[0].BibliographicRecordIdentifierCode.Text)
}

func TestApplyBibliographicUpdateMerge(t *testing.T) {
	original := iso18626.BibliographicInfo{
		SupplierUniqueRecordId: "keep",
		BibliographicRecordId: []iso18626.BibliographicRecordId{
			{
				BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{Text: "location"},
				BibliographicRecordIdentifier:     "Existing",
			},
		},
	}
	updated := ApplyBibliographicUpdate(original, MetadataFields{
		LocalIdentifier: "new",
		Location:        "Main",
		CallNumber:      "123",
	}, directory.Merge)

	assert.Equal(t, "keep", updated.SupplierUniqueRecordId)
	assert.Len(t, updated.BibliographicRecordId, 2)
}

func TestApplyBibliographicUpdateNone(t *testing.T) {
	original := iso18626.BibliographicInfo{SupplierUniqueRecordId: "old"}
	updated := ApplyBibliographicUpdate(original, MetadataFields{LocalIdentifier: "new"}, directory.None)
	assert.Equal(t, "old", updated.SupplierUniqueRecordId)
}
