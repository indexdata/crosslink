package adapter

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/marcxml"
)

type MarcHoldingsParserConfiguration struct {
	MainField                string `json:"mainField"`
	LocationSubField         string `json:"locationSubField"`
	ShelvingLocationSubField string `json:"shelvingLocationSubField"`
	CallNumberSubField       string `json:"callNumberSubField"`
	ItemIdSubField           string `json:"itemIdSubField"`
	RestrictedSubField       string `json:"restrictedSubField"`
}

func MarcHoldingsParserConfigurationNew() *MarcHoldingsParserConfiguration {
	return &MarcHoldingsParserConfiguration{
		MainField:                "852",
		LocationSubField:         "b",
		ShelvingLocationSubField: "c",
		CallNumberSubField:       "h",
		ItemIdSubField:           "p",
		RestrictedSubField:       "r",
	}
}

func (c *MarcHoldingsParserConfiguration) WithMainField(f string) *MarcHoldingsParserConfiguration {
	c.MainField = f
	return c
}

func (c *MarcHoldingsParserConfiguration) WithLocationSubField(f string) *MarcHoldingsParserConfiguration {
	c.LocationSubField = f
	return c
}

func (c *MarcHoldingsParserConfiguration) WithShelvingLocationSubField(f string) *MarcHoldingsParserConfiguration {
	c.ShelvingLocationSubField = f
	return c
}

func (c *MarcHoldingsParserConfiguration) WithCallNumberSubField(f string) *MarcHoldingsParserConfiguration {
	c.CallNumberSubField = f
	return c
}

func (c *MarcHoldingsParserConfiguration) WithItemIdSubField(f string) *MarcHoldingsParserConfiguration {
	c.ItemIdSubField = f
	return c
}

func (c *MarcHoldingsParserConfiguration) WithRestrictedSubField(f string) *MarcHoldingsParserConfiguration {
	c.RestrictedSubField = f
	return c
}

type MarcHoldingsParser struct {
	config MarcHoldingsParserConfiguration
}

func NewMarcHoldingsParser(config *MarcHoldingsParserConfiguration) HoldingsParser {
	if config == nil {
		config = MarcHoldingsParserConfigurationNew()
	}
	return &MarcHoldingsParser{
		config: *config,
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
		if field.Tag == p.config.MainField {
			restricted := false
			var location string
			var shelvingLocation string
			var callNumber string
			var itemId string
			for _, subfield := range field.Subfield {
				switch subfield.Code {
				case p.config.LocationSubField:
					location = strings.TrimSpace(string(subfield.Text))
				case p.config.ShelvingLocationSubField:
					shelvingLocation = strings.TrimSpace(string(subfield.Text))
				case p.config.CallNumberSubField:
					callNumber = strings.TrimSpace(string(subfield.Text))
				case p.config.ItemIdSubField:
					itemId = strings.TrimSpace(string(subfield.Text))
				case p.config.RestrictedSubField:
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
