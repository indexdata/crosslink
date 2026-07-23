package api

import (
	"strings"

	"github.com/google/uuid"

	"github.com/indexdata/crosslink/directory/db"
)

const (
	zoomOptionMockRecords           = "mockRecords"
	zoomOptionPreferredRecordSyntax = "preferredRecordSyntax"
	zoomOptionCount                 = "count"
	zoomOptionElementSetName        = "elementSetName"
	zoomOptionSchema                = "schema"
	zoomOptionAuthentication        = "authentication"
	zoomOptionUser                  = "user"
	zoomOptionPassword              = "password"
	zoomOptionAdapterError          = "adapter-error"
	zoomOptionLookupError           = "lookup-error"
	zoomOptionLocation              = "location"
)

func holdingsConfigToDBParams(entryID uuid.UUID, cfg HoldingsConfig) db.UpsertHoldingsConfigParams {
	params := db.UpsertHoldingsConfigParams{
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
			options := *cfg.Zoom.Options
			params.ZoomOptionMockRecords = stringMapValue(options, zoomOptionMockRecords)
			params.ZoomOptionPreferredRecordSyntax = stringMapValue(options, zoomOptionPreferredRecordSyntax)
			params.ZoomOptionCount = stringMapValue(options, zoomOptionCount)
			params.ZoomOptionElementSetName = stringMapValue(options, zoomOptionElementSetName)
			params.ZoomOptionSchema = stringMapValue(options, zoomOptionSchema)
			params.ZoomOptionAuthentication = stringMapValue(options, zoomOptionAuthentication)
			params.ZoomOptionUser = stringMapValue(options, zoomOptionUser)
			params.ZoomOptionPassword = stringMapValue(options, zoomOptionPassword)
			params.ZoomOptionAdapterError = stringMapValue(options, zoomOptionAdapterError)
			params.ZoomOptionLookupError = stringMapValue(options, zoomOptionLookupError)
			params.ZoomOptionLocation = stringMapValue(options, zoomOptionLocation)
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

func stringMapValue(values map[string]string, key string) *string {
	value, ok := values[key]
	if !ok {
		return nil
	}
	return &value
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

func fullSymbolsToSymbols(values []string) (*[]Symbol, error) {
	if len(values) == 0 {
		return nil, nil
	}
	symbols := make([]Symbol, 0, len(values))
	for _, value := range values {
		authority, symbol, err := resolveCombinedSymbol(value)
		if err != nil {
			return nil, err
		}
		symbols = append(symbols, Symbol{Authority: authority, Symbol: symbol})
	}
	return &symbols, nil
}
