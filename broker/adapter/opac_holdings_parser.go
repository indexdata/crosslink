package adapter

import (
	"encoding/xml"
	"fmt"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/marcxml"
)

type OpacHoldingsParser struct{}

func NewOpacHoldingsParser(config directory.OpacParserConfig) HoldingsParser {
	return &OpacHoldingsParser{}
}

func (p *OpacHoldingsParser) Parse(record []byte, params LookupParams) ([]Holding, error) {
	var opacRecord marcxml.OpacRecord
	err := xml.Unmarshal(record, &opacRecord)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal OPAC XML: %w", err)
	}
	var result []Holding
	for _, holding := range opacRecord.Holdings.Holding {
		availableNow := false
		itemId := ""
		for _, circ := range holding.Circulations.Circulation {
			// regrettably, YAZ uses 0 or 1 to indicate availability, instead of a boolean value
			if circ.AvailableNow.Value == "1" {
				itemId = circ.ItemId
				availableNow = true
				break
			}
		}
		if availableNow {
			result = append(result, Holding{
				Location:         holding.LocalLocation,
				ShelvingLocation: holding.ShelvingLocation,
				CallNumber:       holding.CallNumber,
				ItemId:           itemId,
			})
		}
	}
	return result, nil
}
