package catalog

import (
	"strings"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
)

func fixupBibliographicItem(info *[]iso18626.BibliographicItemId, code string, value string, replace bool) {
	if value == "" {
		return
	}
	for i, id := range *info {
		if id.BibliographicItemIdentifierCode.Text == code {
			if replace || (*info)[i].BibliographicItemIdentifier == "" {
				(*info)[i].BibliographicItemIdentifier = value
			}
			return
		}
	}
	*info = append(*info, iso18626.BibliographicItemId{
		BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: code},
		BibliographicItemIdentifier:     value,
	})
}

func fixupString(src string, dst *string, replace bool) {
	if src == "" {
		return
	}
	if replace || *dst == "" {
		*dst = src
	}
}

func MetadataRequestUpdate(illRequest *iso18626.BibliographicInfo, metadata Metadata, params LookupParams, mode directory.MetadataUpdateMode) error {
	if mode == directory.None {
		return nil
	}
	if mode == directory.Auto {
		if params.Identifier != "" {
			mode = directory.Replace
		} else {
			mode = directory.Merge
		}
	}
	replace := mode == directory.Replace
	fixupString(metadata.Title, &illRequest.Title, replace)
	fixupString(metadata.Subtitle, &illRequest.Subtitle, replace)
	fixupString(metadata.Author, &illRequest.Author, replace)
	fixupString(metadata.Identifier, &illRequest.SupplierUniqueRecordId, replace)
	fixupString(metadata.Edition, &illRequest.Edition, replace)
	fixupBibliographicItem(&illRequest.BibliographicItemId, "ISBN", metadata.Isbn, replace)
	fixupBibliographicItem(&illRequest.BibliographicItemId, "ISSN", metadata.Issn, replace)
	return nil
}

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
