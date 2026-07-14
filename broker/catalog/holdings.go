package catalog

import (
	"strings"

	"github.com/indexdata/crosslink/iso18626"
)

func LookupParamsFromBibliographicInfo(info iso18626.BibliographicInfo, serviceInfo *iso18626.ServiceInfo) LookupParams {
	var serviceType string
	if serviceInfo != nil {
		serviceType = string(serviceInfo.ServiceType)
	}
	params := LookupParams{
		Identifier:  info.SupplierUniqueRecordId,
		Title:       info.Title,
		ServiceType: serviceType,
	}
	for _, id := range info.BibliographicItemId {
		switch strings.TrimSpace(id.BibliographicItemIdentifierCode.Text) {
		case "ISBN":
			params.Isbn = id.BibliographicItemIdentifier
		case "ISSN":
			params.Issn = id.BibliographicItemIdentifier
		}
	}
	return params
}
