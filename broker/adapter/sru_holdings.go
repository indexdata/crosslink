package adapter

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/marcxml"
	"github.com/indexdata/crosslink/sru"
)

type SruHoldingsLookupAdapter struct {
	sruUrl string
	client *http.Client
}

func CreateSruHoldingsLookupAdapter(client *http.Client, sruUrl string) HoldingsLookupAdapter {
	return &SruHoldingsLookupAdapter{client: client, sruUrl: sruUrl}
}

func parseHoldings(rec *marcxml.Record, holdings *[]Holding) {
	// skipped and ignored if there is no 999, which suggests that something is wrong with the record
	for _, df := range rec.Datafield {
		if df.Tag != "999" || df.Ind1 != "1" || df.Ind2 != "0" {
			continue
		}
		var holding Holding
		for _, sf := range df.Subfield {
			if sf.Code == "l" {
				holding.LocalIdentifier = string(sf.Text)
			}
			if sf.Code == "s" {
				holding.Symbol = string(sf.Text)
				*holdings = append(*holdings, holding)
			}
		}
	}
}

func (s *SruHoldingsLookupAdapter) Lookup(params HoldingLookupParams) ([]Holding, error) {
	cql := "id=\"" + params.Identifier + "\"" // TODO: should do proper CQL string escaping
	query := url.QueryEscape(cql)
	// For now, perform just one request and get "all" records
	buf, err := httpclient.GetXml(s.client, s.sruUrl+"?maximumRecords=1000&query="+query)
	if err != nil {
		return nil, err
	}
	var sruResponse sru.SearchRetrieveResponse
	err = xml.Unmarshal(buf, &sruResponse)
	if err != nil {
		return nil, fmt.Errorf("decoding failed: %s", err.Error())
	}
	if sruResponse.Diagnostics != nil {
		diags := sruResponse.Diagnostics.Diagnostic
		if len(diags) > 0 {
			return nil, errors.New(diags[0].Message + ": " + diags[0].Details)
		}
	}
	var holdings []Holding
	if sruResponse.Records != nil {
		for _, record := range sruResponse.Records.Record {
			if record.RecordXMLEscaping != nil && *record.RecordXMLEscaping != sru.RecordXMLEscapingDefinitionXml {
				continue // skipped and ignored.. Fail completely?
			}
			if record.RecordSchema != "info:srw/schema/1/marcxml-v1.1" {
				continue // skipped and ignored.. Fail completely?
			}
			var rec marcxml.Record
			err = xml.Unmarshal(record.RecordData.XMLContent, &rec)
			if err != nil {
				return nil, fmt.Errorf("decoding marcxml failed: %s", err.Error())
			}
			parseHoldings(&rec, &holdings)
		}
	}
	return holdings, nil
}
