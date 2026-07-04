//go:build cgo

package holdings

import (
	"fmt"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/zoom"
)

func cgoEnabled() bool { return true }

type ZoomAvailabilityAdapter struct {
	zurl           string
	options        zoom.Options
	holdingsParser HoldingsParser
	metadataParser MetadataParser
	queryBuilder   LookupQueryBuilder
}

type ZoomLookupResult struct {
	params  LookupParams
	query   string
	records [][]byte
	adapter *ZoomAvailabilityAdapter
}

func (r *ZoomLookupResult) GetQuery() string {
	return r.query
}

func (r *ZoomLookupResult) GetHoldings() ([]Holding, error) {
	var avail []Holding
	for _, record := range r.records {
		h, err := r.adapter.holdingsParser.Parse(record, r.params)
		if err != nil {
			return nil, fmt.Errorf("failed to parse holdings from Z39.50 record: %w", err)
		}
		avail = append(avail, h...)
	}
	return avail, nil
}

func (r *ZoomLookupResult) GetMetadata() (Metadata, error) {
	var metadata Metadata
	if r.adapter.metadataParser == nil {
		return metadata, fmt.Errorf("metadata parser not configured")
	}
	if len(r.records) == 0 {
		return metadata, fmt.Errorf("no records found")
	}
	metadata, err := r.adapter.metadataParser.Parse(r.records[0])
	if err != nil {
		return metadata, fmt.Errorf("failed to parse metadata from Z39.50 record: %w", err)
	}
	return metadata, nil
}

func NewZoomAvailabilityAdapter(config directory.ZoomConfig, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	a := &ZoomAvailabilityAdapter{
		// default options, can be overridden by config.Options
		options: zoom.Options{
			"count":                 "10",
			"presentChunks":         "10",
			"preferredRecordSyntax": "usmarc",
		},
		zurl:           config.Address,
		holdingsParser: holdingsParser,
		metadataParser: metadataParser,
		queryBuilder:   queryBuilder,
	}
	if config.Options != nil {
		for k, v := range *config.Options {
			a.options[k] = v
		}
	}
	return a, nil
}

func (a *ZoomAvailabilityAdapter) searchRetrieve(conn *zoom.Connection, query *zoom.Query, processRecord func([]byte) (bool, error)) (bool, error) {
	set, err := conn.Search(query)
	if err != nil {
		return false, err
	}
	defer set.Close()
	var found bool
	limit := min(set.Count(), 100) // safety limit to avoid processing too many records
	for i := 0; i < limit; i++ {
		rec, err := set.GetRecord(i)
		if err != nil {
			return false, err
		}
		if rec == nil {
			continue
		}
		xmlBuffer := rec.Data("xml;charset=utf-8")
		rec.Close()
		if xmlBuffer == nil {
			continue
		}
		cont, err := processRecord(xmlBuffer)
		if err != nil {
			return false, err
		}
		found = true
		if !cont {
			break
		}
	}
	return found, nil
}

func (a *ZoomAvailabilityAdapter) iterateQueries(
	params LookupParams,
	processRecord func([]byte) (bool, error),
) (string, error) {
	conn := zoom.NewConnection(a.options)
	defer conn.Close()
	var pqfList []string
	var cqlList []string
	if err := conn.Connect(a.zurl); err != nil {
		return "", fmt.Errorf("failed to connect to Z39.50 server: %w", err)
	}
	cqlList, pqfList, err := a.queryBuilder.Build(params)
	if err != nil {
		return "", fmt.Errorf("failed to build query: %w", err)
	}

	if len(pqfList) == 0 && len(cqlList) == 0 {
		return "", fmt.Errorf("no valid query parameters provided")
	}
	for _, pqf := range pqfList {
		query, err := zoom.NewPqfQuery(pqf)
		if err != nil {
			return pqf, fmt.Errorf("failed to create PQF query: %w", err)
		}
		found, err := a.searchRetrieve(conn, query, processRecord)
		query.Close()
		if err != nil {
			return pqf, fmt.Errorf("failed to search server with PQF: %s err %w", pqf, err)
		}
		if found {
			return pqf, nil
		}
	}
	for _, cql := range cqlList {
		query, err := zoom.NewCqlQuery(cql)
		if err != nil {
			return cql, fmt.Errorf("failed to create CQL query: %w", err)
		}
		found, err := a.searchRetrieve(conn, query, processRecord)
		query.Close()
		if err != nil {
			return cql, fmt.Errorf("failed to search server with CQL: %s err %w", cql, err)
		}
		if found {
			return cql, nil
		}
	}
	if len(pqfList) > 0 {
		return pqfList[0], nil
	}
	return cqlList[0], nil
}

func (a *ZoomAvailabilityAdapter) Lookup(params LookupParams) (LookupResult, error) {
	var result ZoomLookupResult
	result.params = params
	result.adapter = a
	var err error
	result.query, err = a.iterateQueries(params, func(xmlBuffer []byte) (bool, error) {
		result.records = append(result.records, xmlBuffer)
		return true, nil // get all records in a search response
	})
	return &result, err
}
