package adapter

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/marcxml"
)

type ReservoirHoldingsParser struct{}

func parseHoldingsForIndicator(rec *marcxml.Record, ind2 string) []Holding {
	var holdings []Holding
	for _, df := range rec.Datafield {
		if df.Tag != "999" || df.Ind1 != "1" || df.Ind2 != ind2 {
			continue
		}
		var holding Holding
		for _, sf := range df.Subfield {
			// l comes before s, so append happens when s is found
			if sf.Code == "l" {
				holding.LocalIdentifier = string(sf.Text)
			}
			if sf.Code == "s" {
				symbol := string(sf.Text)
				if symbol != "" {
					scheme, _, found := strings.Cut(symbol, ":")
					if !found || strings.TrimSpace(scheme) == "" {
						symbol = isilPrefix + symbol
					}
				}
				holding.Symbol = symbol
				holdings = append(holdings, holding)
			}
		}
	}
	return holdings
}

func parseHoldings(rec *marcxml.Record) []Holding {
	// skipped and ignored if there is no 999, which suggests that something is wrong with the record
	holdings := parseHoldingsForIndicator(rec, "1")
	if len(holdings) == 0 {
		holdings = parseHoldingsForIndicator(rec, "0")
	}
	return holdings
}

func (p *ReservoirHoldingsParser) Parse(recordData []byte) ([]Holding, error) {
	var rec marcxml.Record
	err := xml.Unmarshal(recordData, &rec)
	if err != nil {
		return nil, fmt.Errorf("decoding marcxml failed: %s", err.Error())
	}
	return parseHoldings(&rec), nil
}
