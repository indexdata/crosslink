package adapter

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/indexdata/crosslink/broker/httpclient"
	"github.com/indexdata/crosslink/sru"
)

type SruHoldingsLookupAdapter struct {
	sruUrl string
	client *http.Client
}

func CreateSruHoldingsLookupAdapter(client *http.Client, sruUrl string) *SruHoldingsLookupAdapter {
	return &SruHoldingsLookupAdapter{client: client, sruUrl: sruUrl}
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
			return nil, errors.New(diags[0].Message + " " + diags[0].Details)
		}
	}
	var holdings []Holding
	if sruResponse.Records != nil {
		for _, record := range sruResponse.Records.Record {
			if record.RecordXMLEscaping != nil && *record.RecordXMLEscaping != sru.RecordXMLEscapingDefinitionXml {
				continue
			}
			if record.RecordSchema != "info:srw/schema/1/marcxml-v1.1" {
				continue
			}
			holdings = append(holdings, Holding{
				Symbol:          "isil:sup1", // TODO: source from record
				LocalIdentifier: "isil:sup1", // TODO: local identifier from record"
			})
		}
	}
	return holdings, nil
}
