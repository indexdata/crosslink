package holdings

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/sru"
	"github.com/indexdata/crosslink/sru/diag"
)

type SruHoldingsLookupAdapter struct {
	sruUrl         []string
	client         *http.Client
	holdingsParser HoldingsParser
	metadataParser MetadataParser
	queryBuilder   LookupQueryBuilder
	xTarget        string
	recordSchema   string
}

func CreateSruHoldingsLookupAdapter(client *http.Client, sruUrl []string, xTarget string, queryBuilder LookupQueryBuilder, parser HoldingsParser, metadataParser MetadataParser, recordSchema string) LookupAdapter {
	return &SruHoldingsLookupAdapter{client: client, sruUrl: sruUrl, queryBuilder: queryBuilder, holdingsParser: parser, metadataParser: metadataParser, xTarget: xTarget, recordSchema: recordSchema}
}

func NewSruAvailabilityAdapter(config directory.SruConfig, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	var recordSchema string
	if config.RecordSchema != nil {
		recordSchema = *config.RecordSchema
	}
	if recordSchema == "" {
		recordSchema = "marcxml" // default to marcxml if not specified
	}
	return CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{config.Address}, "", queryBuilder, holdingsParser, metadataParser, recordSchema), nil
}

func (s *SruHoldingsLookupAdapter) parseRecord(record *sru.RecordDefinition, params LookupParams, holdings *[]Holding) error {
	if record.RecordXMLEscaping != nil && *record.RecordXMLEscaping != sru.RecordXMLEscapingDefinitionXml {
		return fmt.Errorf("unsupported RecordXMLEscaping: %s", *record.RecordXMLEscaping)
	}
	receivedSchema := record.RecordSchema
	if receivedSchema == "info:srw/schema/1/diagnostics-v1.1" { // surrogate diagnostic record
		var diagnostic diag.Diagnostic
		err := xml.Unmarshal(record.RecordData.XMLContent, &diagnostic)
		if err != nil {
			return fmt.Errorf("decoding surrogate diagnostic failed: %s", err.Error())
		}
		return errors.New("surrogate diagnostic: " + diagnostic.Message + ": " + diagnostic.Details)
	}
	if receivedSchema == "info:srw/schema/1/marcxml-v1.1" {
		receivedSchema = "marcxml"
	}
	if receivedSchema != "" && receivedSchema != s.recordSchema {
		return fmt.Errorf("unsupported RecordSchema: %s", record.RecordSchema)
	}

	ret, err := s.holdingsParser.Parse(record.RecordData.XMLContent, params)
	if err != nil {
		return fmt.Errorf("parsing holdings failed: %s", err.Error())
	}
	*holdings = append(*holdings, ret...)
	return nil
}

func encodeCqlSearchClause(field string, value string) (string, error) {
	cqlQuery, err := cqlbuilder.NewQuery().Search(field).Term(value).Build()
	if err != nil {
		return "", err
	}
	return cqlQuery.String(), nil
}

func (s *SruHoldingsLookupAdapter) search(sruUrl string, params LookupParams, query string) ([]Holding, string, error) {
	var sruResponse sru.SearchRetrieveResponse
	query = "?maximumRecords=1000&recordSchema=" + url.QueryEscape(s.recordSchema) + "&" + query
	if s.xTarget != "" {
		query += "&x-target=" + url.QueryEscape(s.xTarget)
	}
	err := httpclient.NewClient().GetXml(s.client, sruUrl+query, &sruResponse)
	// notice: returning query even in case of error, to allow logging the query that caused the error
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
	var holdings []Holding
	if sruResponse.Records != nil {
		for _, record := range sruResponse.Records.Record {
			err := s.parseRecord(&record, params, &holdings)
			if err != nil {
				return nil, query, err
			}
		}
	}
	return holdings, query, nil
}

func (s *SruHoldingsLookupAdapter) getHoldings(sruUrl string, params LookupParams) ([]Holding, string, error) {
	var holdings []Holding
	cqlList, pqfList, err := s.queryBuilder.Build(params)
	if err != nil {
		return nil, "", err
	}
	var queryParams string
	for _, cql := range cqlList {
		sruQuery := "query=" + url.QueryEscape(cql)
		holdings, queryParams, err = s.search(sruUrl, params, sruQuery)
		if err != nil {
			return nil, queryParams, err
		}
		if len(holdings) > 0 {
			return holdings, queryParams, nil
		}
	}
	for _, pqf := range pqfList {
		sruQuery := "x-pquery=" + url.QueryEscape(pqf)
		holdings, queryParams, err = s.search(sruUrl, params, sruQuery)
		if err != nil {
			return nil, queryParams, err
		}
		if len(holdings) > 0 {
			return holdings, queryParams, nil
		}
	}
	return holdings, queryParams, nil
}

func (s *SruHoldingsLookupAdapter) Lookup(params LookupParams) ([]Holding, string, error) {
	var holdings []Holding
	logQuery := ""
	for _, sruUrl := range s.sruUrl {
		h, query, err := s.getHoldings(sruUrl, params)
		if err != nil {
			return nil, query, err
		}
		holdings = append(holdings, h...)
		logQuery = query
	}
	return holdings, logQuery, nil
}

func (s *SruHoldingsLookupAdapter) MetadataLookup(params LookupParams) (Metadata, error) {
	var metadata Metadata
	return metadata, nil
}
