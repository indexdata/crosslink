package holdings

import (
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
)

func fixupBibliograhpicItem(info *[]iso18626.BibliographicItemId, code string, value string, replace bool) {
	for i, id := range *info {
		if id.BibliographicItemIdentifierCode.Text == code {
			if replace {
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

func MetadataRequestUpdate(illRequest *iso18626.BibliographicInfo, metadata Metadata, mode directory.MetadataUpdateMode) error {
	switch mode {
	case directory.Replace:
		illRequest.Title = metadata.Title
		illRequest.Subtitle = metadata.Subtitle
		illRequest.Author = metadata.Author
		illRequest.SupplierUniqueRecordId = metadata.Identifier
		fixupBibliograhpicItem(&illRequest.BibliographicItemId, "ISBN", metadata.Isbn, true)
		fixupBibliograhpicItem(&illRequest.BibliographicItemId, "ISSN", metadata.Issn, true)
	case directory.Merge:
		if illRequest.Title == "" {
			illRequest.Title = metadata.Title
		}
		if illRequest.Subtitle == "" {
			illRequest.Subtitle = metadata.Subtitle
		}
		if illRequest.Author == "" {
			illRequest.Author = metadata.Author
		}
		if illRequest.SupplierUniqueRecordId == "" {
			illRequest.SupplierUniqueRecordId = metadata.Identifier
		}
		fixupBibliograhpicItem(&illRequest.BibliographicItemId, "ISBN", metadata.Isbn, false)
		fixupBibliograhpicItem(&illRequest.BibliographicItemId, "ISSN", metadata.Issn, false)
	}
	return nil
}
