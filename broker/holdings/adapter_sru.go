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

func (s *SruHoldingsLookupAdapter) parseRecord(record *sru.RecordDefinition, processRecord func([]byte) (bool, error)) (bool, error) {
	if record.RecordXMLEscaping != nil && *record.RecordXMLEscaping != sru.RecordXMLEscapingDefinitionXml {
		return false, fmt.Errorf("unsupported RecordXMLEscaping: %s", *record.RecordXMLEscaping)
	}
	receivedSchema := record.RecordSchema
	if receivedSchema == "info:srw/schema/1/diagnostics-v1.1" { // surrogate diagnostic record
		var diagnostic diag.Diagnostic
		err := xml.Unmarshal(record.RecordData.XMLContent, &diagnostic)
		if err != nil {
			return false, fmt.Errorf("decoding surrogate diagnostic failed: %s", err.Error())
		}
		return false, errors.New("surrogate diagnostic: " + diagnostic.Message + ": " + diagnostic.Details)
	}
	if receivedSchema == "info:srw/schema/1/marcxml-v1.1" {
		receivedSchema = "marcxml"
	}
	if receivedSchema != "" && receivedSchema != s.recordSchema {
		return false, fmt.Errorf("unsupported RecordSchema: %s", record.RecordSchema)
	}
	return processRecord(record.RecordData.XMLContent)
}

func encodeCqlSearchClause(field string, value string) (string, error) {
	cqlQuery, err := cqlbuilder.NewQuery().Search(field).Term(value).Build()
	if err != nil {
		return "", err
	}
	return cqlQuery.String(), nil
}

func (s *SruHoldingsLookupAdapter) search(sruUrl string, params LookupParams, query string, processRecord func([]byte) (bool, error)) (bool, string, error) {
	var sruResponse sru.SearchRetrieveResponse
	query = "?maximumRecords=1000&recordSchema=" + url.QueryEscape(s.recordSchema) + "&" + query
	if s.xTarget != "" {
		query += "&x-target=" + url.QueryEscape(s.xTarget)
	}
	found := false
	err := httpclient.NewClient().GetXml(s.client, sruUrl+query, &sruResponse)
	// notice: returning query even in case of error, to allow logging the query that caused the error
	if err != nil {
		return false, query, err
	}
	if sruResponse.Diagnostics != nil {
		// non-surrogate diagnostics
		diags := sruResponse.Diagnostics.Diagnostic
		if len(diags) > 0 {
			return false, query, errors.New(diags[0].Message + ": " + diags[0].Details)
		}
	}
	if sruResponse.Records != nil {
		for _, record := range sruResponse.Records.Record {
			cont, err := s.parseRecord(&record, processRecord)
			if err != nil {
				return false, query, err
			}
			found = true
			if !cont {
				break
			}
		}
	}
	return found, query, nil
}

func (s *SruHoldingsLookupAdapter) getHoldings(sruUrl string, params LookupParams, processRecord func([]byte) (bool, error)) (bool, string, error) {
	cqlList, pqfList, err := s.queryBuilder.Build(params)
	if err != nil {
		return false, "", err
	}
	var queryParams string
	var found bool
	for _, cql := range cqlList {
		sruQuery := "query=" + url.QueryEscape(cql)
		found, queryParams, err = s.search(sruUrl, params, sruQuery, processRecord)
		if err != nil || found {
			return found, queryParams, err
		}
	}
	for _, pqf := range pqfList {
		sruQuery := "x-pquery=" + url.QueryEscape(pqf)
		found, queryParams, err = s.search(sruUrl, params, sruQuery, processRecord)
		if err != nil || found {
			return found, queryParams, err
		}
	}
	return false, queryParams, nil
}

func (s *SruHoldingsLookupAdapter) HoldingsLookup(params LookupParams) ([]Holding, string, error) {
	var holdings []Holding
	logQuery := ""
	for _, sruUrl := range s.sruUrl {
		var err error
		found, query, err := s.getHoldings(sruUrl, params, func(xmlBuffer []byte) (bool, error) {
			h, err := s.holdingsParser.Parse(xmlBuffer, params)
			if err != nil {
				return false, fmt.Errorf("failed to parse holdings from SRU record: %w", err)
			}
			holdings = append(holdings, h...)
			return true, nil // get all records in a search response
		})
		if err != nil {
			return nil, query, err
		}
		logQuery = query
		if found {
			break
		}
	}
	return holdings, logQuery, nil
}

func (s *SruHoldingsLookupAdapter) MetadataLookup(params LookupParams) (Metadata, error) {
	if s.metadataParser == nil {
		return Metadata{}, fmt.Errorf("metadata parser not configured")
	}
	var metadata Metadata
	for _, sruUrl := range s.sruUrl {
		found, _, err := s.getHoldings(sruUrl, params, func(xmlBuffer []byte) (bool, error) {
			m, err := s.metadataParser.Parse(xmlBuffer)
			if err != nil {
				return false, fmt.Errorf("failed to parse metadata from SRU record: %w", err)
			}
			metadata = m
			return false, nil // get just first record in a search response
		})
		if err != nil {
			return Metadata{}, fmt.Errorf("failed to get metadata from SRU holdings: %w", err)
		}
		if found {
			break
		}
	}
	return metadata, nil
}
