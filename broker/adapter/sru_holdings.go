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
	"github.com/indexdata/crosslink/sru/diag"
)

type SruHoldingsLookupAdapter struct {
	sruUrl []string
	client *http.Client
}

func CreateSruHoldingsLookupAdapter(client *http.Client, sruUrl []string) HoldingsLookupAdapter {
	return &SruHoldingsLookupAdapter{client: client, sruUrl: sruUrl}
}

func parseHoldings(rec *marcxml.Record, holdings *[]Holding) {
	// skipped and ignored if there is no 999, which suggests that something is wrong with the record
	for _, df := range rec.Datafield {
		if df.Tag != "999" || df.Ind1 != "1" || df.Ind2 != "1" {
			continue
		}
		var holding Holding
		for _, sf := range df.Subfield {
			// l comes before s, so append happens when s is found
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

func parseRecord(record *sru.RecordDefinition, holdings *[]Holding) error {
	if record.RecordXMLEscaping != nil && *record.RecordXMLEscaping != sru.RecordXMLEscapingDefinitionXml {
		return fmt.Errorf("unsupported RecordXMLEscapiong: %s", *record.RecordXMLEscaping)
	}
	if record.RecordSchema == "info:srw/schema/1/diagnostics-v1.1" { // surrogate diagnostic record
		var diagnostic diag.Diagnostic
		err := xml.Unmarshal(record.RecordData.XMLContent, &diagnostic)
		if err != nil {
			return fmt.Errorf("decoding surrogate diagnostic failed: %s", err.Error())
		}
		return errors.New("surrogate diagnostic: " + diagnostic.Message + ": " + diagnostic.Details)
	}
	if record.RecordSchema != "info:srw/schema/1/marcxml-v1.1" && record.RecordSchema != "marcxml" {
		return fmt.Errorf("unsupported RecordSchema: %s", record.RecordSchema)
	}
	var rec marcxml.Record
	err := xml.Unmarshal(record.RecordData.XMLContent, &rec)
	if err != nil {
		return fmt.Errorf("decoding marcxml failed: %s", err.Error())
	}
	parseHoldings(&rec, holdings)
	return nil
}

func (s *SruHoldingsLookupAdapter) getHoldings(sruUrl string, identifier string) ([]Holding, string, error) {
	var holdings []Holding
	cql := "rec.id=\"" + identifier + "\"" // TODO: should do proper CQL string escaping
	query := "?maximumRecords=1000&recordSchema=marcxml&query=" + url.QueryEscape(cql)
	var sruResponse sru.SearchRetrieveResponse
	// For now, perform just one request and get "all" records
	err := httpclient.NewClient().GetXml(s.client, sruUrl+query, &sruResponse)
	if err != nil {
		return nil, query, err
	}
	if sruResponse.Diagnostics != nil {
		// non-surrogate diagnostics
		diags := sruResponse.Diagnostics.Diagnostic
		if len(diags) > 0 {
			return nil, query, errors.New(diags[0].Message + ": " + diags[0].Details)
		}
	}
	if sruResponse.Records != nil {
		for _, record := range sruResponse.Records.Record {
			err := parseRecord(&record, &holdings)
			if err != nil {
				return nil, query, err
			}
		}
	}
	return holdings, query, nil
}

func (s *SruHoldingsLookupAdapter) Lookup(params HoldingLookupParams) ([]Holding, string, error) {
	var holdings []Holding
	logQuery := ""
	for _, sruUrl := range s.sruUrl {
		h, query, err := s.getHoldings(sruUrl, params.Identifier)
		if err != nil {
			return nil, query, err
		}
		holdings = append(holdings, h...)
		logQuery = query
	}
	return holdings, logQuery, nil
}
