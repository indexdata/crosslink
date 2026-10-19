package catalog

import (
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestLookupParamsPublicationYear(t *testing.T) {
	info := iso18626.BibliographicInfo{SupplierUniqueRecordId: "record"}
	assert.Empty(t, LookupParamsFromBibliographicInfo(info, nil, nil).Year)
	for _, date := range []string{"", "2021", "2021-05-01"} {
		params := LookupParamsFromBibliographicInfo(info, nil, &iso18626.PublicationInfo{PublicationDate: date})
		assert.Equal(t, date, params.Year) // Validation depends on the catalog's configuration.
		assert.Equal(t, "record", params.Identifier)
	}
}
