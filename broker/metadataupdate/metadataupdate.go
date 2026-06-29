package metadataupdate

import (
	"strings"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
)

const DefaultMetadataFormat = directory.Marc21

type MetadataFields struct {
	LocalIdentifier  string
	Location         string
	ShelvingLocation string
	CallNumber       string
	ItemId           string
}

func SupportsFormat(format directory.MetadataFormat) bool {
	return format == directory.Marc21
}

func ApplyBibliographicUpdate(
	original iso18626.BibliographicInfo,
	metadata MetadataFields,
	mode directory.MetadataUpdateMode,
) iso18626.BibliographicInfo {
	updated := original
	if mode == directory.None {
		return updated
	}

	if mode == directory.Replace {
		updated.SupplierUniqueRecordId = metadata.LocalIdentifier
		updated.BibliographicRecordId = stripCrosslinkMetadataCodes(updated.BibliographicRecordId)
		updated.BibliographicRecordId = appendCrosslinkMetadata(updated.BibliographicRecordId, metadata, false)
		return updated
	}

	if mode == directory.Merge {
		if strings.TrimSpace(updated.SupplierUniqueRecordId) == "" {
			updated.SupplierUniqueRecordId = metadata.LocalIdentifier
		}
		updated.BibliographicRecordId = appendCrosslinkMetadata(updated.BibliographicRecordId, metadata, true)
	}
	return updated
}

func appendCrosslinkMetadata(existing []iso18626.BibliographicRecordId, metadata MetadataFields, mergeOnly bool) []iso18626.BibliographicRecordId {
	values := map[string]string{
		"location":         metadata.Location,
		"shelvingLocation": metadata.ShelvingLocation,
		"callNumber":       metadata.CallNumber,
		"itemId":           metadata.ItemId,
	}
	for code, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if mergeOnly && hasCrosslinkCode(existing, code) {
			continue
		}
		scheme := "crosslink"
		existing = append(existing, iso18626.BibliographicRecordId{
			BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{Text: code, Scheme: &scheme},
			BibliographicRecordIdentifier:     trimmed,
		})
	}
	return existing
}

func stripCrosslinkMetadataCodes(existing []iso18626.BibliographicRecordId) []iso18626.BibliographicRecordId {
	result := make([]iso18626.BibliographicRecordId, 0, len(existing))
	for _, entry := range existing {
		if isCrosslinkMetadataCode(entry.BibliographicRecordIdentifierCode.Text) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func hasCrosslinkCode(existing []iso18626.BibliographicRecordId, code string) bool {
	for _, entry := range existing {
		if strings.EqualFold(strings.TrimSpace(entry.BibliographicRecordIdentifierCode.Text), strings.TrimSpace(code)) {
			return true
		}
	}
	return false
}

func isCrosslinkMetadataCode(code string) bool {
	switch strings.TrimSpace(code) {
	case "location", "shelvingLocation", "callNumber", "itemId":
		return true
	default:
		return false
	}
}
