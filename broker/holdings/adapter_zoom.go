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

func (a *ZoomAvailabilityAdapter) searchRetrieve(params LookupParams, conn *zoom.Connection, query *zoom.Query) ([]Holding, error) {
	set, err := conn.Search(query)
	if err != nil {
		return nil, err
	}
	defer set.Close()
	var avail []Holding
	limit := min(set.Count(), 100) // safety limit to avoid processing too many records
	for i := 0; i < limit; i++ {
		rec, err := set.GetRecord(i)
		if err != nil {
			return nil, err
		}
		if rec == nil {
			continue
		}
		xmlBuffer := rec.Data("xml;charset=utf-8")
		rec.Close()
		if xmlBuffer == nil {
			continue
		}
		holdings, err := a.holdingsParser.Parse(xmlBuffer, params)
		if err != nil {
			return nil, fmt.Errorf("failed to parse holdings from Z39.50 record: %w", err)
		}
		avail = append(avail, holdings...)
	}
	return avail, nil
}

func (a *ZoomAvailabilityAdapter) HoldingsLookup(params LookupParams) ([]Holding, string, error) {
	conn := zoom.NewConnection(a.options)
	defer conn.Close()
	err := conn.Connect(a.zurl)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to Z39.50 server: %w", err)
	}
	cqlList, pqfList, err := a.queryBuilder.Build(params)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build query: %w", err)
	}
	if len(pqfList) == 0 && len(cqlList) == 0 {
		return nil, "", fmt.Errorf("no valid query parameters provided")
	}
	for _, pqf := range pqfList {
		query, err := zoom.NewPqfQuery(pqf)
		if err != nil {
			return nil, pqf, fmt.Errorf("failed to create PQF query: %w", err)
		}
		avail, err := a.searchRetrieve(params, conn, query)
		query.Close()
		if err != nil {
			return nil, pqf, fmt.Errorf("failed to search server with PQF: %s err %w", pqf, err)
		}
		if len(avail) > 0 {
			return avail, pqf, nil
		}
	}
	for _, cql := range cqlList {
		query, err := zoom.NewCqlQuery(cql)
		if err != nil {
			return nil, cql, fmt.Errorf("failed to create CQL query: %w", err)
		}
		avail, err := a.searchRetrieve(params, conn, query)
		query.Close()
		if err != nil {
			return nil, cql, fmt.Errorf("failed to search server with CQL: %s err %w", cql, err)
		}
		if len(avail) > 0 {
			return avail, cql, nil
		}
	}
	if len(pqfList) > 0 {
		return nil, pqfList[0], nil
	}
	return nil, cqlList[0], nil
}

func (a *ZoomAvailabilityAdapter) MetadataLookup(params LookupParams) (Metadata, error) {
	var metadata Metadata
	if a.metadataParser == nil {
		return metadata, fmt.Errorf("metadata parser not configured")
	}
	conn := zoom.NewConnection(a.options)
	defer conn.Close()
	err := conn.Connect(a.zurl)
	if err != nil {
		return metadata, fmt.Errorf("failed to connect to Z39.50 server: %w", err)
	}
	cqlList, pqfList, err := a.queryBuilder.Build(params)
	if err != nil {
		return metadata, fmt.Errorf("failed to build query: %w", err)
	}
	var query *zoom.Query
	if len(pqfList) > 0 {
		query, err = zoom.NewPqfQuery(pqfList[0])
		if err != nil {
			return metadata, fmt.Errorf("failed to create PQF query: %w", err)
		}
	} else if len(cqlList) > 0 {
		query, err = zoom.NewCqlQuery(cqlList[0])
		if err != nil {
			return metadata, fmt.Errorf("failed to create CQL query: %w", err)
		}
	} else {
		return metadata, fmt.Errorf("no valid query parameters provided")
	}
	set, err := conn.Search(query)
	if err != nil {
		return metadata, err
	}
	defer set.Close()
	limit := min(set.Count(), 100) // safety limit to avoid processing too many records
	for i := 0; i < limit; i++ {
		rec, err := set.GetRecord(i)
		if err != nil {
			return metadata, err
		}
		if rec == nil {
			continue
		}
		xmlBuffer := rec.Data("xml;charset=utf-8")
		rec.Close()
		if xmlBuffer == nil {
			continue
		}
		metadata, err = a.metadataParser.Parse(xmlBuffer)
		if err != nil {
			return metadata, fmt.Errorf("failed to parse metadata from Z39.50 record: %w", err)
		}
		return metadata, nil
	}
	return metadata, nil
}
