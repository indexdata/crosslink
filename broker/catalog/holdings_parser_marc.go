package catalog

import (
	"encoding/xml"
	"fmt"
	"slices"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/marcxml"
)

type MarcHoldingsParser struct {
	config dirapi.MarcHoldingsParserConfig
}

func NewMarcHoldingsParser(config dirapi.MarcHoldingsParserConfig) HoldingsParser {
	if config.MainField == nil && config.LocationSubfield == nil && config.ShelvingLocationSubfield == nil && config.CallNumberSubfield == nil && config.ItemIdSubfield == nil && config.RestrictedSubfield == nil && config.Availability == nil && config.ItemLoanPolicySubfield == nil {
		config.MainField = NewString("852")
		config.LocationSubfield = NewString("b")
		config.ShelvingLocationSubfield = NewString("c")
		config.CallNumberSubfield = NewString("h")
		config.ItemIdSubfield = NewString("p")
		config.RestrictedSubfield = NewString("r")
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
			var itemLoanPolicy string
			for _, subfield := range field.Subfield {
				if p.config.LocationSubfield != nil && subfield.Code == *p.config.LocationSubfield {
					location = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.ShelvingLocationSubfield != nil && subfield.Code == *p.config.ShelvingLocationSubfield {
					shelvingLocation = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.CallNumberSubfield != nil && subfield.Code == *p.config.CallNumberSubfield {
					callNumber = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.ItemIdSubfield != nil && subfield.Code == *p.config.ItemIdSubfield {
					itemId = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.ItemLoanPolicySubfield != nil && subfield.Code == *p.config.ItemLoanPolicySubfield {
					itemLoanPolicy = strings.TrimSpace(string(subfield.Text))
				}
				if p.config.RestrictedSubfield != nil && subfield.Code == *p.config.RestrictedSubfield {
					restricted = true
				}
			}
			if p.config.Availability != nil {
				for _, rule := range *p.config.Availability {
					found, matches, excluded := false, false, false
					for _, sub := range field.Subfield {
						if sub.Code == rule.Subfield {
							found = true
							if rule.Values != nil && slices.Contains(*rule.Values, string(sub.Text)) {
								excluded = true
							}
							if rule.Value != nil && string(sub.Text) == *rule.Value {
								matches = true
							}
						}
					}
					if (rule.Operator == "absent" && found) || (rule.Operator == "equals" && !matches) || (rule.Operator == "present" && !found) || (rule.Operator == "notIn" && excluded) {
						restricted = true
					}
				}
			}
			if !restricted && location != "" {
				holdings = append(holdings, Holding{
					Location:         location,
					ShelvingLocation: shelvingLocation,
					CallNumber:       callNumber,
					ItemId:           itemId,
					ItemLoanPolicy:   itemLoanPolicy,
				})
			}
		}
	}
	return holdings, nil
}
