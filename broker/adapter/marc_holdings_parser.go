package adapter

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/marcxml"
)

type MarcHoldingsParser struct {
	mainField                string
	locationSubField         string
	shelvingLocationSubField string
	callNumberSubField       string
	itemIdSubField           string
	restrictedSubField       string
}

func NewMarcHoldingsParser() HoldingsParser {
	return &MarcHoldingsParser{
		mainField:                "852",
		locationSubField:         "b",
		shelvingLocationSubField: "c",
		callNumberSubField:       "h",
		itemIdSubField:           "p",
		restrictedSubField:       "r",
	}
}

func NewMarcHoldingsParserCfg(mainField string, locationField string, shelvingLocationField string, callNumberField string, itemIdField string, restrictedField string) HoldingsParser {
	return &MarcHoldingsParser{
		mainField:                mainField,
		locationSubField:         locationField,
		shelvingLocationSubField: shelvingLocationField,
		callNumberSubField:       callNumberField,
		itemIdSubField:           itemIdField,
		restrictedSubField:       restrictedField,
	}
}

func (p *MarcHoldingsParser) Parse(record []byte) ([]Holding, error) {
	var marcRecord marcxml.Record
	err := xml.Unmarshal(record, &marcRecord)
	// TODO : consider OPAC record as well
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal MARC XML: %w", err)
	}
	var holdings []Holding
	for _, field := range marcRecord.Datafield {
		if field.Tag == p.mainField {
			restricted := false
			var location string
			var shelvingLocation string
			var callNumber string
			var itemId string
			for _, subfield := range field.Subfield {
				switch subfield.Code {
				case p.locationSubField:
					location = strings.TrimSpace(string(subfield.Text))
				case p.shelvingLocationSubField:
					shelvingLocation = strings.TrimSpace(string(subfield.Text))
				case p.callNumberSubField:
					callNumber = strings.TrimSpace(string(subfield.Text))
				case p.itemIdSubField:
					itemId = strings.TrimSpace(string(subfield.Text))
				case p.restrictedSubField:
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
