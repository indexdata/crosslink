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

func (p *OpacHoldingsParser) Parse(record []byte, _ LookupParams) ([]Holding, error) {
	var opacRecord marcxml.OpacRecord
	if err := xml.Unmarshal(record, &opacRecord); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OPAC XML: %w", err)
	}
	var result []Holding
	for _, holding := range opacRecord.Holdings.Holding {
		if p.config.ExcludedLocations != nil && slices.Contains(*p.config.ExcludedLocations, strings.TrimSpace(holding.LocalLocation)) {
			continue
		}
		if enabled(p.config.RequireLocalLocation, false) && strings.TrimSpace(holding.LocalLocation) == "" {
			continue
		}
		base := Holding{Location: holding.LocalLocation, ShelvingLocation: holding.ShelvingLocation, CallNumber: holding.CallNumber}
		if p.config.ShelvingLocationSource != nil && *p.config.ShelvingLocationSource == "localLocation" {
			base.ShelvingLocation = holding.LocalLocation
		}
		if enabled(p.config.TrimWhitespace, false) {
			base.Location = strings.TrimSpace(base.Location)
			base.ShelvingLocation = strings.TrimSpace(base.ShelvingLocation)
			base.CallNumber = strings.TrimSpace(base.CallNumber)
		}
		if p.config.AvailabilityRule != nil && *p.config.AvailabilityRule == "publicNote" {
			if !enabled(p.config.RequireItemLoanPolicy, false) && p.config.AvailablePublicNotes != nil && slices.Contains(*p.config.AvailablePublicNotes, holding.PublicNote) {
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
				if enabled(p.config.TrimWhitespace, false) {
					h.ItemId = strings.TrimSpace(h.ItemId)
				}
			}
			if enabled(p.config.IncludeTemporaryLocation, false) {
				// Supplier policy and ordering consume the effective shelving location.
				if temporaryLocation := strings.TrimSpace(circ.TemporaryLocation); temporaryLocation != "" {
					h.ShelvingLocation = temporaryLocation
				}
			}
			// Sources produce separate candidates; they are not a fallback chain.
			sources := []string{"availableThru"}
			if p.config.ItemLoanPolicySources != nil {
				sources = *p.config.ItemLoanPolicySources
			}
			emitted := false
			if enabled(p.config.IncludeItemLoanPolicy, true) {
				for _, source := range sources {
					var policy string
					switch source {
					case "availableThru":
						policy = circ.AvailableThru
					case "shelvingData":
						policy = holding.ShelvingData
					case "temporaryLocation":
						policy = circ.TemporaryLocation
					}
					if policy = strings.TrimSpace(policy); policy != "" {
						h.ItemLoanPolicy = policy
						result = append(result, h)
						emitted = true
					}
				}
			}
			if !emitted && !enabled(p.config.RequireItemLoanPolicy, false) {
				result = append(result, h)
			}
		}
	}
	return result, nil
}
