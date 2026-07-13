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

type SruLookupResult struct {
	query    string
	holdings []Holding
	metadata *Metadata
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

func (s *SruHoldingsLookupAdapter) search(sruUrl string, params LookupParams, query string, processRecord func([]byte) (bool, error)) (bool, error) {
	var sruResponse sru.SearchRetrieveResponse
	query = "?maximumRecords=1000&recordSchema=" + url.QueryEscape(s.recordSchema) + "&" + query
	if s.xTarget != "" {
		query += "&x-target=" + url.QueryEscape(s.xTarget)
	}
	found := false
	err := httpclient.NewClient().GetXml(s.client, sruUrl+query, &sruResponse)
	// notice: returning query even in case of error, to allow logging the query that caused the error
	if err != nil {
		return false, err
	}
	if sruResponse.Diagnostics != nil {
		// non-surrogate diagnostics
		diags := sruResponse.Diagnostics.Diagnostic
		if len(diags) > 0 {
			return false, errors.New(diags[0].Message + ": " + diags[0].Details)
		}
	}
	if sruResponse.Records != nil {
		for _, record := range sruResponse.Records.Record {
			foundRecord, err := s.parseRecord(&record, processRecord)
			if err != nil {
				return false, fmt.Errorf("failed to parse holdings from SRU record: %w", err)
			}
			if foundRecord {
				found = true
			}
		}
	}
	return found, nil
}

func (s *SruHoldingsLookupAdapter) lookupServer(sruUrl string, params LookupParams, processRecord func([]byte) (bool, error)) (bool, string, error) {
	cqlList, pqfList, err := s.queryBuilder.Build(params)
	if err != nil {
		return false, "", err
	}
	var query string
	var found bool
	for _, cql := range cqlList {
		query = cql
		sruQuery := "query=" + url.QueryEscape(cql)
		found, err = s.search(sruUrl, params, sruQuery, processRecord)
		if err != nil || found {
			return found, query, err
		}
	}
	for _, pqf := range pqfList {
		query = pqf
		sruQuery := "x-pquery=" + url.QueryEscape(pqf)
		found, err = s.search(sruUrl, params, sruQuery, processRecord)
		if err != nil || found {
			return found, query, err
		}
	}
	return false, query, nil
}

func (s *SruHoldingsLookupAdapter) Lookup(params LookupParams) (LookupResult, error) {
	var result SruLookupResult

	for _, sruUrl := range s.sruUrl {
		var err error
		found, query, err := s.lookupServer(sruUrl, params, func(xmlBuffer []byte) (bool, error) {
			h, err := s.holdingsParser.Parse(xmlBuffer, params)
			if err != nil {
				return false, err
			}
			if result.metadata == nil && s.metadataParser != nil {
				metadata, err := s.metadataParser.Parse(xmlBuffer)
				if err != nil {
					return false, fmt.Errorf("failed to parse metadata from SRU record: %w", err)
				}
				result.metadata = &metadata
			}
			if len(h) == 0 {
				return false, nil
			}
			result.holdings = append(result.holdings, h...)
			return true, nil
		})
		result.query = query
		if err != nil {
			return &result, err
		}
		if found {
			break
		}
	}
	return &result, nil
}

func (r *SruLookupResult) GetQuery() string {
	return r.query
}

func (s *SruLookupResult) GetHoldings() ([]Holding, error) {
	return s.holdings, nil
}

func (s *SruLookupResult) GetMetadata() (Metadata, error) {
	if s.metadata == nil {
		return Metadata{}, nil
	}
	return *s.metadata, nil
}
