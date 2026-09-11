package catalog

import (
	"encoding/xml"
	"fmt"
	"slices"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/marcxml"
)

type OpacHoldingsParser struct {
	config dirapi.OpacHoldingsParserConfig
}

func NewOpacHoldingsParser(config dirapi.OpacHoldingsParserConfig) HoldingsParser {
	return &OpacHoldingsParser{config: config}
}
func enabled(value *bool, fallback bool) bool {
	if value != nil {
		return *value
	}
	return fallback
}

func (p *OpacHoldingsParser) Parse(record []byte, params LookupParams) ([]Holding, error) {
	var opacRecord marcxml.OpacRecord
	if err := xml.Unmarshal(record, &opacRecord); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OPAC XML: %w", err)
	}
	var result []Holding
	for _, holding := range opacRecord.Holdings.Holding {
		if enabled(p.config.RequireLocalLocation, false) && strings.TrimSpace(holding.LocalLocation) == "" {
			continue
		}
		base := Holding{Location: holding.LocalLocation, ShelvingLocation: holding.ShelvingLocation, CallNumber: holding.CallNumber}
		if p.config.ShelvingLocationSource != nil && *p.config.ShelvingLocationSource == "localLocation" {
			base.ShelvingLocation = holding.LocalLocation
		}
		if p.config.AvailabilityRule != nil && *p.config.AvailabilityRule == "publicNote" {
			if p.config.AvailablePublicNotes != nil && slices.Contains(*p.config.AvailablePublicNotes, holding.PublicNote) {
				result = append(result, base)
			}
			continue
		}
		if holding.Circulations == nil {
			continue
		}
		for _, circ := range holding.Circulations.Circulation {
			if circ.AvailableNow.Value != "1" {
				continue
			}
			h := base
			if enabled(p.config.IncludeItemId, true) {
				h.ItemId = circ.ItemId
			}
			if enabled(p.config.IncludeItemLoanPolicy, true) {
				h.ItemLoanPolicy = strings.TrimSpace(circ.AvailableThru)
			}
			if enabled(p.config.IncludeTemporaryLocation, false) {
				h.TemporaryShelvingLocation = circ.TemporaryLocation
			}
			result = append(result, h)
			if !enabled(p.config.AllCirculations, false) {
				break
			}
		}
	}
	return result, nil
}
