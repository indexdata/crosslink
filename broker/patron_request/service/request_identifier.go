package prservice

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/lms"
	"github.com/indexdata/crosslink/iso18626"
)

// ISO supplierInfo carries the selected copy separately from the bibliographic
// supplierUniqueRecordId. Conflicting copy IDs must not silently select a copy.
func requestItemIdentifierFromISO(request iso18626.Request) (lms.RequestItemIdentifier, error) {
	id := lms.RequestItemIdentifier{BibliographicID: request.BibliographicInfo.SupplierUniqueRecordId}
	for _, supplier := range request.SupplierInfo {
		if supplier.BibliographicRecordId == nil {
			continue
		}
		item := supplier.BibliographicRecordId.BibliographicRecordIdentifier
		if strings.TrimSpace(item) == "" {
			return id, fmt.Errorf("empty supplierInfo item identifier")
		}
		if id.ItemID != "" && id.ItemID != item {
			return id, fmt.Errorf("conflicting supplierInfo item identifiers")
		}
		id.ItemID = item
	}
	return id, nil
}
