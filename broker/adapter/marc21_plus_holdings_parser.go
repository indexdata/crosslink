package adapter

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/marcxml"
)

type Marc21Plus1HoldingsParser struct {
	config directory.MarcParserConfig
}

func NewMarc21Plus1HoldingsParser() HoldingsParser {
	return &Marc21Plus1HoldingsParser{}
}

func (p *Marc21Plus1HoldingsParser) Parse(record []byte) ([]Holding, error) {
	var marcRecord marcxml.Record
	err := xml.Unmarshal(record, &marcRecord)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal MARC XML: %w", err)
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
