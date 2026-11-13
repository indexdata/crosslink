package prservice

import (
	"fmt"
	"strings"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
)

const sharedIndexRecordCode = "ReShare"

// BibRecordID returns a patron request's shared-index record ID, or "" if it has none
// or it is ambiguous. On the borrowing side that is supplierUniqueRecordId. The broker
// replaces that field with the supplier's local ID in the requests it sends and adds the
// shared-index ID under code ReShare, so on the lending side it is read from there.
func BibRecordID(side pr_db.PatronRequestSide, illRequest iso18626.Request) string {
	if side == SideBorrowing {
		return illRequest.BibliographicInfo.SupplierUniqueRecordId
	}
	if side != SideLending {
		return ""
	}
	id, err := sharedIndexRecordID(illRequest.BibliographicInfo)
	if err != nil {
		return ""
	}
	return id
}

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
// stale IDs when the source identifier has been cleared. It does not mutate the input slice.
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
