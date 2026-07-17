package catalog

import (
	"encoding/xml"
	"fmt"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/marcxml"
)

type MarcHoldingsParser struct {
	config dirapi.MarcHoldingsParserConfig
}

func NewMarcHoldingsParser(config dirapi.MarcHoldingsParserConfig) HoldingsParser {
	if config.MainField == nil && config.LocationSubField == nil && config.ShelvingLocationSubField == nil && config.CallNumberSubField == nil && config.ItemIdSubField == nil && config.RestrictedSubField == nil {
		config.MainField = NewString("852")
		config.LocationSubField = NewString("b")
		config.ShelvingLocationSubField = NewString("c")
		config.CallNumberSubField = NewString("h")
		config.ItemIdSubField = NewString("p")
		config.RestrictedSubField = NewString("r")
	}
	// perhaps should check if mainField is specified
	return &MarcHoldingsParser{
		config: config,
	}
}

func (p *MarcHoldingsParser) Parse(record []byte, params LookupParams) ([]Holding, error) {
	// Now parse the MARC record, try with the MARC21 slim namespace first, then without it if that fails
	var marcRecord marcxml.Record
	err := xml.Unmarshal(record, &marcRecord)
	if err != nil {
		// GVI marc does not have the MARC21 slim namespace, so we try again without it
		var noNamespaceRecord marcxml.RecordType
		err = xml.Unmarshal(record, &noNamespaceRecord)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal MARC XML: %w", err)
		}
		marcRecord.RecordType = noNamespaceRecord
	}
	var holdings []Holding
	for _, field := range marcRecord.Datafield {
		if p.config.MainField != nil && field.Tag == *p.config.MainField {
			restricted := false
			var location string
			var shelvingLocation string
			var callNumber string
			var itemId string
			for _, subfield := range field.Subfield {
				if p.config.LocationSubField != nil && subfield.Code == *p.config.LocationSubField {
					location = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.ShelvingLocationSubField != nil && subfield.Code == *p.config.ShelvingLocationSubField {
					shelvingLocation = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.CallNumberSubField != nil && subfield.Code == *p.config.CallNumberSubField {
					callNumber = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.ItemIdSubField != nil && subfield.Code == *p.config.ItemIdSubField {
					itemId = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.RestrictedSubField != nil && subfield.Code == *p.config.RestrictedSubField {
					restricted = true
				}
			}
			if !restricted && location != "" {
				holdings = append(holdings, Holding{
					Location:         location,
					ShelvingLocation: shelvingLocation,
					CallNumber:       callNumber,
					ItemId:           itemId,
				})
			}
		}
	}
	return holdings, nil
}
