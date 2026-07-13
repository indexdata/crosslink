//go:build cgo

package catalog

import (
	"fmt"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/zoom"
)

func cgoEnabled() bool { return true }

type ZoomLookupAdapter struct {
	zurl           string
	options        zoom.Options
	holdingsParser HoldingsParser
	metadataParser MetadataParser
	queryBuilder   LookupQueryBuilder
}

type ZoomLookupResult struct {
	query    string
	holdings []Holding
	metadata *Metadata
}

func NewZoomLookupAdapter(config directory.ZoomConfig, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	a := &ZoomLookupAdapter{
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

func (a *ZoomLookupAdapter) searchRetrieve(conn *zoom.Connection, query *zoom.Query, processRecord func([]byte) (bool, error)) (bool, error) {
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
		foundRecord, err := processRecord(xmlBuffer)
		if err != nil {
			return false, err
		}
		if foundRecord {
			found = true
		}
	}
	return found, nil
}

func (a *ZoomLookupAdapter) iterateQueries(
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

func (a *ZoomLookupAdapter) Lookup(params LookupParams) (LookupResult, error) {
	var result ZoomLookupResult
	var err error
	result.query, err = a.iterateQueries(params, func(xmlBuffer []byte) (bool, error) {
		h, err := a.holdingsParser.Parse(xmlBuffer, params)
		if err != nil {
			return false, fmt.Errorf("failed to parse holdings from ZOOM record: %w", err)
		}
		if result.metadata == nil && a.metadataParser != nil {
			newMetadata, err := a.metadataParser.Parse(xmlBuffer)
			if err != nil {
				return false, fmt.Errorf("failed to parse metadata from ZOOM record: %w", err)
			}
			result.metadata = &newMetadata
		}
		if len(h) == 0 {
			return false, nil
		}
		result.holdings = append(result.holdings, h...)
		return true, nil
	})
	return &result, err
}

func (r *ZoomLookupResult) GetQuery() string {
	return r.query
}

func (r *ZoomLookupResult) GetHoldings() ([]Holding, error) {
	return r.holdings, nil
}

func (r *ZoomLookupResult) GetMetadata() (Metadata, error) {
	if r.metadata == nil {
		return Metadata{}, nil
	}
	return *r.metadata, nil
}
