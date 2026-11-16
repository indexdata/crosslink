package prservice

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/iso18626"
)

const sharedIndexRecordCode = "ReShare"

// sharedIndexRecordID reads only the consortium's record identifier. Supplier
// bibliographic IDs and copy IDs belong to different namespaces.
func sharedIndexRecordID(info iso18626.BibliographicInfo) (string, error) {
	var id string
	for _, record := range info.BibliographicRecordId {
		if !strings.EqualFold(record.BibliographicRecordIdentifierCode.Text, sharedIndexRecordCode) {
			continue
		}
		value := record.BibliographicRecordIdentifier
		if value == "" {
			continue
		}
		if id != "" && id != value {
			return "", fmt.Errorf("conflicting ReShare bibliographic record IDs")
		}
		id = value
	}
	return id, nil
}

// setSharedIndexRecordID replaces only ReShare identifiers, including removing
// stale IDs when the local field has been cleared. It does not mutate the input slice.
func setSharedIndexRecordID(info *iso18626.BibliographicInfo, id string) {
	var records []iso18626.BibliographicRecordId
	for _, record := range info.BibliographicRecordId {
		if !strings.EqualFold(record.BibliographicRecordIdentifierCode.Text, sharedIndexRecordCode) {
			records = append(records, record)
		}
	}
	if id != "" {
		records = append(records, iso18626.BibliographicRecordId{
			BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{Text: sharedIndexRecordCode},
			BibliographicRecordIdentifier:     id,
		})
	}
	info.BibliographicRecordId = records
}
