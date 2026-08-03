package api

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/indexdata/crosslink/directory/db"
)

func catalogConfigToDBParams(entryID uuid.UUID, cfg CatalogConfig) db.UpsertCatalogConfigParams {
	params := db.UpsertCatalogConfigParams{
		Entry: &entryID,
	}

	if cfg.MetadataUpdateMode != nil {
		value := string(*cfg.MetadataUpdateMode)
		params.MetadataUpdateMode = &value
	}
	if cfg.Sru != nil {
		params.SruAddress = &cfg.Sru.Address
		params.SruRecordSchema = cfg.Sru.RecordSchema
	}
	if cfg.Zoom != nil {
		params.ZoomAddress = &cfg.Zoom.Address
		if cfg.Zoom.Options != nil {
			params.ZoomOptions, _ = json.Marshal(*cfg.Zoom.Options)
		}
	}
	if cfg.QueryConfig != nil {
		if cfg.QueryConfig.Type != nil {
			value := string(*cfg.QueryConfig.Type)
			params.QueryType = &value
		}
		params.QueryIdentifier = cfg.QueryConfig.Identifier
		params.QueryIsbn = cfg.QueryConfig.Isbn
		params.QueryIssn = cfg.QueryConfig.Issn
		params.QueryTitle = cfg.QueryConfig.Title
	}
	if cfg.HoldingsFormat != nil {
		if cfg.HoldingsFormat.Marc != nil {
			marc := cfg.HoldingsFormat.Marc
			params.HoldingsMarcCallNumberSubfield = marc.CallNumberSubField
			params.HoldingsMarcItemIDSubfield = marc.ItemIdSubField
			params.HoldingsMarcLocationSubfield = marc.LocationSubField
			params.HoldingsMarcMainField = marc.MainField
			params.HoldingsMarcRestrictedSubfield = marc.RestrictedSubField
			params.HoldingsMarcShelvingLocationSubfield = marc.ShelvingLocationSubField
		}
		params.HoldingsMarc21plus1Enabled = boolPtr(cfg.HoldingsFormat.Marc21plus1 != nil)
		params.HoldingsOpacEnabled = boolPtr(cfg.HoldingsFormat.Opac != nil)
		params.HoldingsReservoirEnabled = boolPtr(cfg.HoldingsFormat.Reservoir != nil)
	}
	if cfg.MetadataFormat != nil && cfg.MetadataFormat.Marc21 != nil {
		marc := cfg.MetadataFormat.Marc21
		params.MetadataMarc21Author = marc.Author
		params.MetadataMarc21Edition = marc.Edition
		params.MetadataMarc21Identifier = marc.Identifier
		params.MetadataMarc21Isbn = marc.Isbn
		params.MetadataMarc21Issn = marc.Issn
		params.MetadataMarc21Subtitle = marc.Subtitle
		params.MetadataMarc21Title = marc.Title
	}

	return params
}

func holdingsPolicyJSON(policy HoldingsPolicy) []byte {
	value, _ := json.Marshal(policy)
	return value
}

func illConfigToDBParams(entryID uuid.UUID, cfg IllConfig) db.UpsertIllConfigParams {
	params := db.UpsertIllConfigParams{
		Entry:                       entryID,
		Iso18626Url:                 cfg.Iso18626Url,
		LendersOfLastResort:         symbolsToFullSymbols(cfg.LendersOfLastResort),
		IncludeRequestingAgencyInfo: cfg.IncludeRequestingAgencyInfo,
		IncludeSupplierInfo:         cfg.IncludeSupplierInfo,
		IncludeReturnInfo:           cfg.IncludeReturnInfo,
		IncludeVendorNote:           cfg.IncludeVendorNote,
		UseOfferedCosts:             cfg.UseOfferedCosts,
		NoteFieldSeparator:          cfg.NoteFieldSeparator,
		SupplierPatronPattern:       cfg.SupplierPatronPattern,
		DuplicateCheckWindowHours:   cfg.DuplicateCheckWindowHours,
	}
	if cfg.Iso18626Vendor != nil {
		vendor := string(*cfg.Iso18626Vendor)
		params.Iso18626Vendor = &vendor
	}
	return params
}

func boolPtr(value bool) *bool {
	return &value
}

func symbolsToFullSymbols(symbols *[]Symbol) []string {
	if symbols == nil || len(*symbols) == 0 {
		return nil
	}
	values := make([]string, 0, len(*symbols))
	for _, symbol := range *symbols {
		values = append(values, strings.ToUpper(symbol.Authority)+":"+strings.ToUpper(symbol.Symbol))
	}
	return values
}
