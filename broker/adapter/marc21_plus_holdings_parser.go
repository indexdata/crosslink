package adapter

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/crosslink/marcxml"
)

// Holding Information to be used for routing is part of
// repeatable MARC 924 fields (one for each holding library).

// First Indicator  Resource Type

// "0"  Non-electronic (= default)

// "1"  Electronic

// $a (NR) Local IDN of the holding record
// $b (NR) ISIL as an identifier of the owning institution
// $c (NR) Interlibrary loan region
// $d (NR) Interlibrary loan indicator
//            "a" - Loan of volumes possible, no copies
//            "b" - No loan of volumes, only paper copies are sent
//            "c" - Unrestricted interlibrary loan, copying and loan
//            "d" - No interlibrary loan
//            "e" - No loan of volumes, the end user receives an
//                   electronic copy
// $k (R)  Electronic address (URL) for a remotely accessed file
// $1 (R)  Identification "Produktsigel" for national licenses
//          and digital collections, so called "ProduktSigel"
//          (it is an ISIL according to the German ISIL-Agency)

// Full documentation Result format is MARC21, see from Deutsche
// Nationalbibliothek (DNB), https://d-nb.info/1282570226/34

type Marc21Plus1HoldingsParser struct {
}

func NewMarc21Plus1HoldingsParser() HoldingsParser {
	return &Marc21Plus1HoldingsParser{}
}

func (p *Marc21Plus1HoldingsParser) Parse(record []byte, params LookupParams) ([]Holding, error) {
	var marcRecord marcxml.Record
	err := xml.Unmarshal(record, &marcRecord)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal MARC XML: %w", err)
	}
	loanOk := params.ServiceType == string(iso18626.TypeServiceTypeLoan) ||
		params.ServiceType == string(iso18626.TypeServiceTypeCopyOrLoan) || params.ServiceType == ""
	copyOk := params.ServiceType == string(iso18626.TypeServiceTypeCopy) ||
		params.ServiceType == string(iso18626.TypeServiceTypeCopyOrLoan) || params.ServiceType == ""

	var holdings []Holding
	for _, field := range marcRecord.Datafield {
		if field.Tag == "924" {
			var localIdentifier string
			var symbol string
			ok := false
			for _, subfield := range field.Subfield {
				if subfield.Code == "a" {
					localIdentifier = strings.TrimSpace(string(subfield.Text))
				}
				if subfield.Code == "b" {
					symbol = strings.TrimSpace(string(subfield.Text))
				}
				if subfield.Code == "d" { // loan indicator
					indicator := strings.TrimSpace(string(subfield.Text))
					if indicator == "a" {
						ok = loanOk
					}
					if indicator == "b" {
						ok = copyOk
					}
					if indicator == "c" { // unrestricted interlibrary loan, so we can treat it as available
						ok = true
					}
					if indicator == "e" {
						ok = copyOk
					}
				}
			}
			if ok && localIdentifier != "" && symbol != "" {
				holdings = append(holdings, Holding{
					LocalIdentifier: localIdentifier,
					Symbol:          symbol,
				})
			}
		}
	}
	return holdings, nil
}
