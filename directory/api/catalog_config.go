package api

import (
	"encoding/json"
	"errors"
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

func validateCatalogConfigPatch(cfg CatalogConfigPatch, original db.CatalogConfig) error {
	if cfg.Sru != nil && cfg.Sru.Address == nil && original.SruAddress == nil {
		return errors.New("catalogConfig.sru.address is required when creating SRU configuration")
	}
	if cfg.Zoom != nil && cfg.Zoom.Address == nil && original.ZoomAddress == nil {
		return errors.New("catalogConfig.zoom.address is required when creating ZOOM configuration")
	}
	return nil
}

func catalogConfigPatchToDBParams(entryID uuid.UUID, cfg CatalogConfigPatch, original db.CatalogConfig) (db.UpsertCatalogConfigParams, error) {
	params := db.UpsertCatalogConfigParams{
		Entry:                                &entryID,
		MetadataUpdateMode:                   original.MetadataUpdateMode,
		SruAddress:                           original.SruAddress,
		SruRecordSchema:                      original.SruRecordSchema,
		ZoomAddress:                          original.ZoomAddress,
		ZoomOptions:                          original.ZoomOptions,
		QueryType:                            original.QueryType,
		QueryIdentifier:                      original.QueryIdentifier,
		QueryIsbn:                            original.QueryIsbn,
		QueryIssn:                            original.QueryIssn,
		QueryTitle:                           original.QueryTitle,
		HoldingsMarcCallNumberSubfield:       original.HoldingsMarcCallNumberSubfield,
		HoldingsMarcItemIDSubfield:           original.HoldingsMarcItemIDSubfield,
		HoldingsMarcLocationSubfield:         original.HoldingsMarcLocationSubfield,
		HoldingsMarcMainField:                original.HoldingsMarcMainField,
		HoldingsMarcRestrictedSubfield:       original.HoldingsMarcRestrictedSubfield,
		HoldingsMarcShelvingLocationSubfield: original.HoldingsMarcShelvingLocationSubfield,
		HoldingsMarc21plus1Enabled:           original.HoldingsMarc21plus1Enabled,
		HoldingsOpacEnabled:                  original.HoldingsOpacEnabled,
		HoldingsReservoirEnabled:             original.HoldingsReservoirEnabled,
		MetadataMarc21Author:                 original.MetadataMarc21Author,
		MetadataMarc21Edition:                original.MetadataMarc21Edition,
		MetadataMarc21Identifier:             original.MetadataMarc21Identifier,
		MetadataMarc21Isbn:                   original.MetadataMarc21Isbn,
		MetadataMarc21Issn:                   original.MetadataMarc21Issn,
		MetadataMarc21Subtitle:               original.MetadataMarc21Subtitle,
		MetadataMarc21Title:                  original.MetadataMarc21Title,
	}

	if cfg.MetadataUpdateMode != nil {
		value := string(*cfg.MetadataUpdateMode)
		params.MetadataUpdateMode = &value
	}
	if cfg.Sru != nil {
		params.SruAddress = derefOrDefaultPtr(cfg.Sru.Address, params.SruAddress)
		params.SruRecordSchema = derefOrDefaultPtr(cfg.Sru.RecordSchema, params.SruRecordSchema)
	}
	if cfg.Zoom != nil {
		params.ZoomAddress = derefOrDefaultPtr(cfg.Zoom.Address, params.ZoomAddress)
		if cfg.Zoom.Options != nil {
			options := make(map[string]string)
			if len(params.ZoomOptions) > 0 {
				if err := json.Unmarshal(params.ZoomOptions, &options); err != nil {
					return db.UpsertCatalogConfigParams{}, err
				}
				if options == nil {
					options = make(map[string]string)
				}
			}
			for key, value := range *cfg.Zoom.Options {
				if value == nil {
					delete(options, key)
				} else {
					options[key] = *value
				}
			}
			var err error
			params.ZoomOptions, err = json.Marshal(options)
			if err != nil {
				return db.UpsertCatalogConfigParams{}, err
			}
		}
	}
	if cfg.QueryConfig != nil {
		if cfg.QueryConfig.Type != nil {
			value := string(*cfg.QueryConfig.Type)
			params.QueryType = &value
		}
		params.QueryIdentifier = derefOrDefaultPtr(cfg.QueryConfig.Identifier, params.QueryIdentifier)
		params.QueryIsbn = derefOrDefaultPtr(cfg.QueryConfig.Isbn, params.QueryIsbn)
		params.QueryIssn = derefOrDefaultPtr(cfg.QueryConfig.Issn, params.QueryIssn)
		params.QueryTitle = derefOrDefaultPtr(cfg.QueryConfig.Title, params.QueryTitle)
	}
	if cfg.HoldingsFormat != nil {
		if cfg.HoldingsFormat.Marc != nil {
			marc := cfg.HoldingsFormat.Marc
			params.HoldingsMarcCallNumberSubfield = derefOrDefaultPtr(marc.CallNumberSubField, params.HoldingsMarcCallNumberSubfield)
			params.HoldingsMarcItemIDSubfield = derefOrDefaultPtr(marc.ItemIdSubField, params.HoldingsMarcItemIDSubfield)
			params.HoldingsMarcLocationSubfield = derefOrDefaultPtr(marc.LocationSubField, params.HoldingsMarcLocationSubfield)
			params.HoldingsMarcMainField = derefOrDefaultPtr(marc.MainField, params.HoldingsMarcMainField)
			params.HoldingsMarcRestrictedSubfield = derefOrDefaultPtr(marc.RestrictedSubField, params.HoldingsMarcRestrictedSubfield)
			params.HoldingsMarcShelvingLocationSubfield = derefOrDefaultPtr(marc.ShelvingLocationSubField, params.HoldingsMarcShelvingLocationSubfield)
		}
		if cfg.HoldingsFormat.Marc21plus1 != nil {
			params.HoldingsMarc21plus1Enabled = boolPtr(true)
		}
		if cfg.HoldingsFormat.Opac != nil {
			params.HoldingsOpacEnabled = boolPtr(true)
		}
		if cfg.HoldingsFormat.Reservoir != nil {
			params.HoldingsReservoirEnabled = boolPtr(true)
		}
	}
	if cfg.MetadataFormat != nil && cfg.MetadataFormat.Marc21 != nil {
		marc := cfg.MetadataFormat.Marc21
		params.MetadataMarc21Author = derefOrDefaultPtr(marc.Author, params.MetadataMarc21Author)
		params.MetadataMarc21Edition = derefOrDefaultPtr(marc.Edition, params.MetadataMarc21Edition)
		params.MetadataMarc21Identifier = derefOrDefaultPtr(marc.Identifier, params.MetadataMarc21Identifier)
		params.MetadataMarc21Isbn = derefOrDefaultPtr(marc.Isbn, params.MetadataMarc21Isbn)
		params.MetadataMarc21Issn = derefOrDefaultPtr(marc.Issn, params.MetadataMarc21Issn)
		params.MetadataMarc21Subtitle = derefOrDefaultPtr(marc.Subtitle, params.MetadataMarc21Subtitle)
		params.MetadataMarc21Title = derefOrDefaultPtr(marc.Title, params.MetadataMarc21Title)
	}

	return params, nil
}

func holdingsPolicyJSON(policy HoldingsPolicy) []byte {
	value, _ := json.Marshal(policy)
	return value
}

func mergeHoldingsPolicy(original []byte, patch HoldingsPolicy) (HoldingsPolicy, error) {
	merged := HoldingsPolicy{}
	if len(original) > 0 {
		if err := json.Unmarshal(original, &merged); err != nil {
			return HoldingsPolicy{}, err
		}
	}

	merged.Locations = derefOrDefaultPtr(patch.Locations, merged.Locations)
	merged.ShelvingLocations = derefOrDefaultPtr(patch.ShelvingLocations, merged.ShelvingLocations)
	merged.LocationPolicies = derefOrDefaultPtr(patch.LocationPolicies, merged.LocationPolicies)
	merged.ItemLoanPolicies = derefOrDefaultPtr(patch.ItemLoanPolicies, merged.ItemLoanPolicies)
	return merged, nil
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

func illConfigPatchToDBParams(entryID uuid.UUID, cfg IllConfig, original db.IllConfig) db.UpsertIllConfigParams {
	params := db.UpsertIllConfigParams{
		Entry:                       entryID,
		Iso18626Url:                 original.Iso18626Url,
		Iso18626Vendor:              original.Iso18626Vendor,
		LendersOfLastResort:         original.LendersOfLastResort,
		IncludeRequestingAgencyInfo: original.IncludeRequestingAgencyInfo,
		IncludeSupplierInfo:         original.IncludeSupplierInfo,
		IncludeReturnInfo:           original.IncludeReturnInfo,
		IncludeVendorNote:           original.IncludeVendorNote,
		UseOfferedCosts:             original.UseOfferedCosts,
		NoteFieldSeparator:          original.NoteFieldSeparator,
		SupplierPatronPattern:       original.SupplierPatronPattern,
		DuplicateCheckWindowHours:   original.DuplicateCheckWindowHours,
	}

	params.Iso18626Url = derefOrDefaultPtr(cfg.Iso18626Url, params.Iso18626Url)
	if cfg.Iso18626Vendor != nil {
		vendor := string(*cfg.Iso18626Vendor)
		params.Iso18626Vendor = &vendor
	}
	if cfg.LendersOfLastResort != nil {
		params.LendersOfLastResort = symbolsToFullSymbols(cfg.LendersOfLastResort)
	}
	params.IncludeRequestingAgencyInfo = derefOrDefaultPtr(cfg.IncludeRequestingAgencyInfo, params.IncludeRequestingAgencyInfo)
	params.IncludeSupplierInfo = derefOrDefaultPtr(cfg.IncludeSupplierInfo, params.IncludeSupplierInfo)
	params.IncludeReturnInfo = derefOrDefaultPtr(cfg.IncludeReturnInfo, params.IncludeReturnInfo)
	params.IncludeVendorNote = derefOrDefaultPtr(cfg.IncludeVendorNote, params.IncludeVendorNote)
	params.UseOfferedCosts = derefOrDefaultPtr(cfg.UseOfferedCosts, params.UseOfferedCosts)
	params.NoteFieldSeparator = derefOrDefaultPtr(cfg.NoteFieldSeparator, params.NoteFieldSeparator)
	params.SupplierPatronPattern = derefOrDefaultPtr(cfg.SupplierPatronPattern, params.SupplierPatronPattern)
	params.DuplicateCheckWindowHours = derefOrDefaultPtr(cfg.DuplicateCheckWindowHours, params.DuplicateCheckWindowHours)
	return params
}

func boolPtr(value bool) *bool {
	return &value
}

func symbolsToFullSymbols(symbols *[]Symbol) []string {
	if symbols == nil {
		return nil
	}
	values := make([]string, 0, len(*symbols))
	for _, symbol := range *symbols {
		values = append(values, strings.ToUpper(symbol.Authority)+":"+strings.ToUpper(symbol.Symbol))
	}
	return values
}
