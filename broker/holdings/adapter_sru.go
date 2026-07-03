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

func (s *SruHoldingsLookupAdapter) parseRecord(record *sru.RecordDefinition, params LookupParams, processRecord func([]byte) error) error {
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

	return processRecord(record.RecordData.XMLContent)
}

func encodeCqlSearchClause(field string, value string) (string, error) {
	cqlQuery, err := cqlbuilder.NewQuery().Search(field).Term(value).Build()
	if err != nil {
		return "", err
	}
	return cqlQuery.String(), nil
}

func (s *SruHoldingsLookupAdapter) search(sruUrl string, params LookupParams, query string, processRecord func([]byte) error, shouldContinueQuery func() bool) (string, error) {
	var sruResponse sru.SearchRetrieveResponse
	query = "?maximumRecords=1000&recordSchema=" + url.QueryEscape(s.recordSchema) + "&" + query
	if s.xTarget != "" {
		query += "&x-target=" + url.QueryEscape(s.xTarget)
	}
	err := httpclient.NewClient().GetXml(s.client, sruUrl+query, &sruResponse)
	// notice: returning query even in case of error, to allow logging the query that caused the error
	if err != nil {
		return query, err
	}
	if sruResponse.Diagnostics != nil {
		// non-surrogate diagnostics
		diags := sruResponse.Diagnostics.Diagnostic
		if len(diags) > 0 {
			return query, errors.New(diags[0].Message + ": " + diags[0].Details)
		}
	}
	if sruResponse.Records != nil {
		for _, record := range sruResponse.Records.Record {
			err := s.parseRecord(&record, params, processRecord)
			if err != nil {
				return query, err
			}
			if !shouldContinueQuery() {
				return query, nil
			}
		}
	}
	return query, nil
}

func (s *SruHoldingsLookupAdapter) getHoldings(sruUrl string, params LookupParams, processRecord func([]byte) error, shouldContinueQuery func() bool) (string, error) {
	cqlList, pqfList, err := s.queryBuilder.Build(params)
	if err != nil {
		return "", err
	}
	var queryParams string
	for _, cql := range cqlList {
		sruQuery := "query=" + url.QueryEscape(cql)
		queryParams, err = s.search(sruUrl, params, sruQuery, processRecord, shouldContinueQuery)
		if err != nil {
			return queryParams, err
		}
		if !shouldContinueQuery() {
			return queryParams, nil
		}
	}
	for _, pqf := range pqfList {
		sruQuery := "x-pquery=" + url.QueryEscape(pqf)
		queryParams, err = s.search(sruUrl, params, sruQuery, processRecord, shouldContinueQuery)
		if err != nil {
			return queryParams, err
		}
		if !shouldContinueQuery() {
			return queryParams, nil
		}
	}
	return queryParams, nil
}

func (s *SruHoldingsLookupAdapter) HoldingsLookup(params LookupParams) ([]Holding, string, error) {
	var holdings []Holding
	logQuery := ""
	for _, sruUrl := range s.sruUrl {
		query, err := s.getHoldings(sruUrl, params, func(xmlBuffer []byte) error {
			h, err := s.holdingsParser.Parse(xmlBuffer, params)
			if err != nil {
				return fmt.Errorf("failed to parse holdings from Z39.50 record: %w", err)
			}
			holdings = append(holdings, h...)
			return nil
		}, func() bool {
			return true
		})
		if err != nil {
			return nil, query, err
		}
		logQuery = query
	}
	return holdings, logQuery, nil
}

func (s *SruHoldingsLookupAdapter) MetadataLookup(params LookupParams) (Metadata, error) {
	if s.metadataParser == nil {
		return Metadata{}, fmt.Errorf("metadata parser not configured")
	}
	var metadata Metadata
	cont := true
	for _, sruUrl := range s.sruUrl {
		_, err := s.getHoldings(sruUrl, params, func(xmlBuffer []byte) error {
			m, err := s.metadataParser.Parse(xmlBuffer)
			if err != nil {
				return fmt.Errorf("failed to parse metadata from SRU record: %w", err)
			}
			metadata = m
			cont = false
			return nil
		}, func() bool {
			return cont
		})
		if err != nil {
			return Metadata{}, fmt.Errorf("failed to get metadata from SRU holdings: %w", err)
		}
		if !cont {
			break
		}
	}
	return metadata, nil
}
