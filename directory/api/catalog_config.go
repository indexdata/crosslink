package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/indexdata/crosslink/directory/db"
)

func catalogConfigToDBParams(entryID uuid.UUID, cfg CatalogConfig) db.UpsertCatalogConfigParams {
	params := db.UpsertCatalogConfigParams{
		Entry:   &entryID,
		Profile: maybeUpdateCol[string](nil, cfg.Profile),
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
		params.QueryYear = cfg.QueryConfig.Year
		params.QueryIdentifierNormalization = cfg.QueryConfig.IdentifierNormalization
		params.QueryIdentifierPrefix = cfg.QueryConfig.IdentifierPrefix
	}
	if cfg.HoldingsFormat != nil {
		params.HoldingsConfig, _ = json.Marshal(cfg.HoldingsFormat)
		if cfg.HoldingsFormat.Marc != nil {
			marc := cfg.HoldingsFormat.Marc
			params.HoldingsMarcCallNumberSubfield = marc.CallNumberSubfield
			params.HoldingsMarcItemIDSubfield = marc.ItemIdSubfield
			params.HoldingsMarcLocationSubfield = marc.LocationSubfield
			params.HoldingsMarcMainField = marc.MainField
			params.HoldingsMarcRestrictedSubfield = marc.RestrictedSubfield
			params.HoldingsMarcShelvingLocationSubfield = marc.ShelvingLocationSubfield
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
		params.MetadataMarc21Publisher = marc.Publisher
		params.MetadataMarc21PublicationDate = marc.PublicationDate
		params.MetadataMarc21PlaceOfPublication = marc.PlaceOfPublication
		params.MetadataMarc21Title = marc.Title
	}

	return params
}

func validateCatalogConfigParams(params db.UpsertCatalogConfigParams) error {
	if params.SruAddress != nil && params.ZoomAddress != nil {
		return errors.New("catalogConfig cannot configure both SRU and ZOOM endpoints")
	}
	if len(params.HoldingsConfig) > 0 {
		var holdings HoldingsParserConfig
		if err := json.Unmarshal(params.HoldingsConfig, &holdings); err != nil {
			return err
		}
		count := 0
		for _, present := range []bool{holdings.Marc != nil, holdings.Opac != nil, holdings.Reservoir != nil, holdings.Marc21plus1 != nil} {
			if present {
				count++
			}
		}
		if m := holdings.Marc; m != nil && m.Availability != nil {
			for _, rule := range *m.Availability {
				if (rule.Operator == "equals" && rule.Value == nil) || (rule.Operator == "notIn" && rule.Values == nil) {
					return errors.New("catalogConfig.holdingsFormat.marc.availability requires value for equals and values for notIn")
				}
			}
		}
		if count > 1 {
			return errors.New("catalogConfig.holdingsFormat must set at most one of marc, opac, reservoir, or marc21plus1")
		}
	}
	return nil
}

func validateCatalogConfigPatch(cfg CatalogConfigPatch, original db.CatalogConfig) error {
	if sru, ok := patchValue(cfg.Sru); ok && sru.Address == nil && original.SruAddress == nil {
		return errors.New("catalogConfig.sru.address is required when creating SRU configuration")
	}
	if zoom, ok := patchValue(cfg.Zoom); ok && zoom.Address == nil && original.ZoomAddress == nil {
		return errors.New("catalogConfig.zoom.address is required when creating ZOOM configuration")
	}
	return nil
}

// patchValue returns a specified, non-null patch value.
func patchValue[T any](patch nullable.Nullable[T]) (*T, bool) {
	value, err := patch.Get()
	if err != nil {
		return nil, false
	}
	return &value, true
}

// clearsValue reports whether a patch explicitly removes a value.
func clearsValue[T any](patch nullable.Nullable[T]) bool {
	return patch.IsSpecified() && patch.IsNull()
}

func catalogConfigPatchToDBParams(entryID uuid.UUID, cfg CatalogConfigPatch, original db.CatalogConfig) (db.UpsertCatalogConfigParams, error) {
	params := db.UpsertCatalogConfigParams{
		Entry:                                &entryID,
		Profile:                              maybeUpdateCol(original.Profile, cfg.Profile),
		HoldingsConfig:                       original.HoldingsConfig,
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
		QueryYear:                            original.QueryYear,
		QueryIdentifierNormalization:         original.QueryIdentifierNormalization,
		QueryIdentifierPrefix:                original.QueryIdentifierPrefix,
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
		MetadataMarc21Publisher:              original.MetadataMarc21Publisher,
		MetadataMarc21PublicationDate:        original.MetadataMarc21PublicationDate,
		MetadataMarc21PlaceOfPublication:     original.MetadataMarc21PlaceOfPublication,
		MetadataMarc21Title:                  original.MetadataMarc21Title,
	}

	if clearsValue(cfg.MetadataUpdateMode) {
		params.MetadataUpdateMode = nil
	} else if mode, ok := patchValue(cfg.MetadataUpdateMode); ok {
		value := string(*mode)
		params.MetadataUpdateMode = &value
	}
	if clearsValue(cfg.Sru) {
		params.SruAddress, params.SruRecordSchema = nil, nil
	} else if sru, ok := patchValue(cfg.Sru); ok {
		params.SruAddress = derefOrDefaultPtr(sru.Address, params.SruAddress)
		params.SruRecordSchema = derefOrDefaultPtr(sru.RecordSchema, params.SruRecordSchema)
	}
	if clearsValue(cfg.Zoom) {
		params.ZoomAddress, params.ZoomOptions = nil, nil
	} else if zoom, ok := patchValue(cfg.Zoom); ok {
		params.ZoomAddress = derefOrDefaultPtr(zoom.Address, params.ZoomAddress)
		if zoom.Options != nil {
			options := make(map[string]string)
			if len(params.ZoomOptions) > 0 {
				if err := json.Unmarshal(params.ZoomOptions, &options); err != nil {
					return db.UpsertCatalogConfigParams{}, err
				}
				if options == nil {
					options = make(map[string]string)
				}
			}
			for key, value := range *zoom.Options {
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
	if clearsValue(cfg.QueryConfig) {
		params.QueryType, params.QueryIdentifier, params.QueryIsbn, params.QueryIssn = nil, nil, nil, nil
		params.QueryTitle, params.QueryYear, params.QueryIdentifierNormalization, params.QueryIdentifierPrefix = nil, nil, nil, nil
	} else if query, ok := patchValue(cfg.QueryConfig); ok {
		if query.Type != nil {
			value := string(*query.Type)
			params.QueryType = &value
		}
		params.QueryIdentifier = derefOrDefaultPtr(query.Identifier, params.QueryIdentifier)
		params.QueryIsbn = derefOrDefaultPtr(query.Isbn, params.QueryIsbn)
		params.QueryIssn = derefOrDefaultPtr(query.Issn, params.QueryIssn)
		params.QueryTitle = derefOrDefaultPtr(query.Title, params.QueryTitle)
		params.QueryYear = derefOrDefaultPtr(query.Year, params.QueryYear)
		params.QueryIdentifierNormalization = derefOrDefaultPtr(query.IdentifierNormalization, params.QueryIdentifierNormalization)
		params.QueryIdentifierPrefix = derefOrDefaultPtr(query.IdentifierPrefix, params.QueryIdentifierPrefix)
	}
	if clearsValue(cfg.HoldingsFormat) {
		params.HoldingsConfig = nil
		params.HoldingsMarcCallNumberSubfield, params.HoldingsMarcItemIDSubfield, params.HoldingsMarcLocationSubfield = nil, nil, nil
		params.HoldingsMarcMainField, params.HoldingsMarcRestrictedSubfield, params.HoldingsMarcShelvingLocationSubfield = nil, nil, nil
		params.HoldingsMarc21plus1Enabled, params.HoldingsOpacEnabled, params.HoldingsReservoirEnabled = nil, nil, nil
	} else if holdings, ok := patchValue(cfg.HoldingsFormat); ok {
		var mergeErr error
		params.HoldingsConfig, mergeErr = mergeHoldingsConfig(original.HoldingsConfig, holdings)
		if mergeErr != nil {
			return params, mergeErr
		}
		if holdings.Marc != nil {
			marc := holdings.Marc
			params.HoldingsMarcCallNumberSubfield = derefOrDefaultPtr(marc.CallNumberSubfield, params.HoldingsMarcCallNumberSubfield)
			params.HoldingsMarcItemIDSubfield = derefOrDefaultPtr(marc.ItemIdSubfield, params.HoldingsMarcItemIDSubfield)
			params.HoldingsMarcLocationSubfield = derefOrDefaultPtr(marc.LocationSubfield, params.HoldingsMarcLocationSubfield)
			params.HoldingsMarcMainField = derefOrDefaultPtr(marc.MainField, params.HoldingsMarcMainField)
			params.HoldingsMarcRestrictedSubfield = derefOrDefaultPtr(marc.RestrictedSubfield, params.HoldingsMarcRestrictedSubfield)
			params.HoldingsMarcShelvingLocationSubfield = derefOrDefaultPtr(marc.ShelvingLocationSubfield, params.HoldingsMarcShelvingLocationSubfield)
		}
		if holdings.Marc21plus1 != nil {
			params.HoldingsMarc21plus1Enabled = boolPtr(true)
		}
		if holdings.Opac != nil {
			params.HoldingsOpacEnabled = boolPtr(true)
		}
		if holdings.Reservoir != nil {
			params.HoldingsReservoirEnabled = boolPtr(true)
		}
	}
	if clearsValue(cfg.MetadataFormat) {
		params.MetadataMarc21Author, params.MetadataMarc21Edition, params.MetadataMarc21Identifier = nil, nil, nil
		params.MetadataMarc21Isbn, params.MetadataMarc21Issn, params.MetadataMarc21Subtitle = nil, nil, nil
		params.MetadataMarc21Publisher, params.MetadataMarc21PublicationDate = nil, nil
		params.MetadataMarc21PlaceOfPublication, params.MetadataMarc21Title = nil, nil
	} else if metadata, ok := patchValue(cfg.MetadataFormat); ok && metadata.Marc21 != nil {
		marc := metadata.Marc21
		params.MetadataMarc21Author = derefOrDefaultPtr(marc.Author, params.MetadataMarc21Author)
		params.MetadataMarc21Edition = derefOrDefaultPtr(marc.Edition, params.MetadataMarc21Edition)
		params.MetadataMarc21Identifier = derefOrDefaultPtr(marc.Identifier, params.MetadataMarc21Identifier)
		params.MetadataMarc21Isbn = derefOrDefaultPtr(marc.Isbn, params.MetadataMarc21Isbn)
		params.MetadataMarc21Issn = derefOrDefaultPtr(marc.Issn, params.MetadataMarc21Issn)
		params.MetadataMarc21Subtitle = derefOrDefaultPtr(marc.Subtitle, params.MetadataMarc21Subtitle)
		params.MetadataMarc21Publisher = derefOrDefaultPtr(marc.Publisher, params.MetadataMarc21Publisher)
		params.MetadataMarc21PublicationDate = derefOrDefaultPtr(marc.PublicationDate, params.MetadataMarc21PublicationDate)
		params.MetadataMarc21PlaceOfPublication = derefOrDefaultPtr(marc.PlaceOfPublication, params.MetadataMarc21PlaceOfPublication)
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

// encodeIllConfigCodes preserves omitted fields, stores empty lists as [], and clears a null default.
func encodeIllConfigCodes(params *db.UpsertIllConfigParams, cfg IllConfig) error {
	if cfg.DefaultCopyrightType.IsSpecified() {
		params.DefaultCopyrightType = nil
		if value, err := cfg.DefaultCopyrightType.Get(); err == nil {
			data, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("encode illConfig.defaultCopyrightType: %w", err)
			}
			params.DefaultCopyrightType = data
		}
	}
	for _, field := range []struct {
		name   string
		codes  *[]IllConfigCodeDescription
		target *[]byte
	}{
		{"availableCopyrightTypes", cfg.AvailableCopyrightTypes, &params.AvailableCopyrightTypes},
		{"unfilledReasons", cfg.UnfilledReasons, &params.UnfilledReasons},
		{"cancellationReasons", cfg.CancellationReasons, &params.CancellationReasons},
		{"loanConditions", cfg.LoanConditions, &params.LoanConditions},
	} {
		if field.codes == nil {
			continue
		}
		data, err := json.Marshal(*field.codes)
		if err != nil {
			return fmt.Errorf("encode illConfig.%s: %w", field.name, err)
		}
		*field.target = data
	}
	return nil
}

func illConfigToDBParams(entryID uuid.UUID, cfg IllConfig) (db.UpsertIllConfigParams, error) {
	params := db.UpsertIllConfigParams{
		Entry:                       entryID,
		IsPickupLocation:            cfg.IsPickupLocation,
		Iso18626Url:                 cfg.Iso18626Url,
		LendersOfLastResort:         symbolsToFullSymbols(cfg.LendersOfLastResort),
		IncludeRequestingAgencyInfo: cfg.IncludeRequestingAgencyInfo,
		IncludeSupplierInfo:         cfg.IncludeSupplierInfo,
		IncludeReturnInfo:           cfg.IncludeReturnInfo,
		IncludeVendorNote:           cfg.IncludeVendorNote,
		UseOfferedCosts:             cfg.UseOfferedCosts,
		NoteFieldSeparator:          cfg.NoteFieldSeparator,
		SupplierPatronPattern:       cfg.SupplierPatronPattern,
		RequestIDPrefix:             cfg.RequestIdPrefix,
		DuplicateCheckWindowHours:   cfg.DuplicateCheckWindowHours,
		DefaultLoanPeriod:           nullableLoanPeriod(cfg),
		MaxRequestsPerPatron:        nullableMaxRequestsPerPatron(cfg),
		MinimumCost:                 nullableMinimumCost(cfg),
		LoadBalancingPolicy:         nullableLoadBalancingPolicy(cfg),
	}
	if cfg.Iso18626Vendor != nil {
		vendor := string(*cfg.Iso18626Vendor)
		params.Iso18626Vendor = &vendor
	}
	if err := encodeIllConfigCodes(&params, cfg); err != nil {
		return db.UpsertIllConfigParams{}, err
	}
	return params, nil
}

// illConfigPatchToDBParams applies a patch to the stored local row. Omitted
// fields keep their stored value and null removes it.
func illConfigPatchToDBParams(entryID uuid.UUID, cfg IllConfigPatch, original db.IllConfig) (db.UpsertIllConfigParams, error) {
	params := db.UpsertIllConfigParams{
		Entry:                       entryID,
		DefaultCopyrightType:        original.DefaultCopyrightType,
		AvailableCopyrightTypes:     original.AvailableCopyrightTypes,
		UnfilledReasons:             original.UnfilledReasons,
		LoanConditions:              original.LoanConditions,
		CancellationReasons:         original.CancellationReasons,
		IsPickupLocation:            maybeUpdateCol(original.IsPickupLocation, cfg.IsPickupLocation),
		Iso18626Url:                 maybeUpdateCol(original.Iso18626Url, cfg.Iso18626Url),
		Iso18626Vendor:              original.Iso18626Vendor,
		LendersOfLastResort:         original.LendersOfLastResort,
		IncludeRequestingAgencyInfo: maybeUpdateCol(original.IncludeRequestingAgencyInfo, cfg.IncludeRequestingAgencyInfo),
		IncludeSupplierInfo:         maybeUpdateCol(original.IncludeSupplierInfo, cfg.IncludeSupplierInfo),
		IncludeReturnInfo:           maybeUpdateCol(original.IncludeReturnInfo, cfg.IncludeReturnInfo),
		IncludeVendorNote:           maybeUpdateCol(original.IncludeVendorNote, cfg.IncludeVendorNote),
		UseOfferedCosts:             maybeUpdateCol(original.UseOfferedCosts, cfg.UseOfferedCosts),
		NoteFieldSeparator:          maybeUpdateCol(original.NoteFieldSeparator, cfg.NoteFieldSeparator),
		SupplierPatronPattern:       maybeUpdateCol(original.SupplierPatronPattern, cfg.SupplierPatronPattern),
		RequestIDPrefix:             maybeUpdateCol(original.RequestIDPrefix, cfg.RequestIdPrefix),
		DuplicateCheckWindowHours:   maybeUpdateCol(original.DuplicateCheckWindowHours, cfg.DuplicateCheckWindowHours),
		DefaultLoanPeriod:           maybeUpdateCol(original.DefaultLoanPeriod, cfg.DefaultLoanPeriod),
		MaxRequestsPerPatron:        maybeUpdateCol(original.MaxRequestsPerPatron, cfg.MaxRequestsPerPatron),
		MinimumCost:                 maybeUpdateCol(original.MinimumCost, cfg.MinimumCost),
		LoadBalancingPolicy:         original.LoadBalancingPolicy,
	}
	if clearsValue(cfg.Iso18626Vendor) {
		params.Iso18626Vendor = nil
	} else if vendor, ok := patchValue(cfg.Iso18626Vendor); ok {
		value := string(*vendor)
		params.Iso18626Vendor = &value
	}
	if clearsValue(cfg.LendersOfLastResort) {
		params.LendersOfLastResort = nil
	} else if lenders, ok := patchValue(cfg.LendersOfLastResort); ok {
		params.LendersOfLastResort = symbolsToFullSymbols(lenders)
	}
	if clearsValue(cfg.LoadBalancingPolicy) {
		params.LoadBalancingPolicy = nil
	} else if policy, ok := patchValue(cfg.LoadBalancingPolicy); ok {
		value := string(*policy)
		params.LoadBalancingPolicy = &value
	}
	if cfg.DefaultCopyrightType.IsSpecified() {
		params.DefaultCopyrightType = nil
		if value, ok := patchValue(cfg.DefaultCopyrightType); ok {
			data, err := json.Marshal(value)
			if err != nil {
				return db.UpsertIllConfigParams{}, fmt.Errorf("encode illConfig.defaultCopyrightType: %w", err)
			}
			params.DefaultCopyrightType = data
		}
	}
	for _, field := range []struct {
		name   string
		codes  nullable.Nullable[[]IllConfigCodeDescription]
		target *[]byte
	}{
		{"availableCopyrightTypes", cfg.AvailableCopyrightTypes, &params.AvailableCopyrightTypes},
		{"unfilledReasons", cfg.UnfilledReasons, &params.UnfilledReasons},
		{"cancellationReasons", cfg.CancellationReasons, &params.CancellationReasons},
		{"loanConditions", cfg.LoanConditions, &params.LoanConditions},
	} {
		if !field.codes.IsSpecified() {
			continue
		}
		*field.target = nil
		if codes, ok := patchValue(field.codes); ok {
			data, err := json.Marshal(*codes)
			if err != nil {
				return db.UpsertIllConfigParams{}, fmt.Errorf("encode illConfig.%s: %w", field.name, err)
			}
			*field.target = data
		}
	}
	return params, nil
}

func nullableLoanPeriod(cfg IllConfig) *int32 {
	days, err := cfg.DefaultLoanPeriod.Get()
	if err != nil {
		return nil
	}
	return &days
}

func nullableMaxRequestsPerPatron(cfg IllConfig) *int32 {
	value, err := cfg.MaxRequestsPerPatron.Get()
	if err != nil {
		return nil
	}
	return &value
}

func nullableMinimumCost(cfg IllConfig) *float64 {
	value, err := cfg.MinimumCost.Get()
	if err != nil {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func symbolsToFullSymbols(symbols *[]SymbolProperties) []string {
	if symbols == nil {
		return nil
	}
	values := make([]string, 0, len(*symbols))
	for _, symbol := range *symbols {
		values = append(values, strings.ToUpper(symbol.Authority)+":"+strings.ToUpper(symbol.Symbol))
	}
	return values
}

// mergeHoldingsConfig merges administrator overrides only. Defaults never enter persistence.
func mergeHoldingsConfig(original []byte, patch *HoldingsParserConfig) ([]byte, error) {
	var old map[string]map[string]json.RawMessage
	if len(original) > 0 {
		if err := json.Unmarshal(original, &old); err != nil {
			return nil, err
		}
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	var next map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &next); err != nil {
		return nil, err
	}
	if len(next) == 0 {
		return original, nil
	}
	if len(next) == 1 {
		for parser, fields := range next {
			if prev, ok := old[parser]; ok {
				for key, val := range fields {
					prev[key] = val
				}
				next[parser] = prev
			}
		}
	}
	return json.Marshal(next)
}

func nullableLoadBalancingPolicy(cfg IllConfig) *string {
	value, err := cfg.LoadBalancingPolicy.Get()
	if err != nil {
		return nil
	}
	policy := string(value)
	return &policy
}
