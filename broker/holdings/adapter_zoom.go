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

// searchRetrieve executes a Z39.50 search and iterates over the result records,
// calling processRecord for each XML buffer. processRecord should return true to
// continue iterating and false to stop early (e.g. after the first metadata hit).
func (a *ZoomAvailabilityAdapter) searchRetrieve(conn *zoom.Connection, query *zoom.Query, processRecord func([]byte) (bool, error)) error {
	set, err := conn.Search(query)
	if err != nil {
		return err
	}
	defer set.Close()
	limit := min(set.Count(), 100) // safety limit to avoid processing too many records
	for i := 0; i < limit; i++ {
		rec, err := set.GetRecord(i)
		if err != nil {
			return err
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
			return err
		}
		if !cont {
			break
		}
	}
	return nil
}

// iterateQueries drives the PQF-then-CQL query loop, calling searchRetrieve for
// each query with processRecord. After each query it calls shouldContinueQuery; if
// shouldContinueQuery returns false the loop stops and the current query string is
// returned. Both HoldingsLookup and MetadataLookup use this to share the iteration
// logic while differing only in their per-record gathering and stop condition.
func (a *ZoomAvailabilityAdapter) iterateQueries(
	params LookupParams,
	processRecord func([]byte) (bool, error),
	shouldContinueQuery func() bool,
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
		err = a.searchRetrieve(conn, query, processRecord)
		query.Close()
		if err != nil {
			return pqf, fmt.Errorf("failed to search server with PQF: %s err %w", pqf, err)
		}
		if !shouldContinueQuery() {
			return pqf, nil
		}
	}
	for _, cql := range cqlList {
		query, err := zoom.NewCqlQuery(cql)
		if err != nil {
			return cql, fmt.Errorf("failed to create CQL query: %w", err)
		}
		err = a.searchRetrieve(conn, query, processRecord)
		query.Close()
		if err != nil {
			return cql, fmt.Errorf("failed to search server with CQL: %s err %w", cql, err)
		}
		if !shouldContinueQuery() {
			return cql, nil
		}
	}
	if len(pqfList) > 0 {
		return pqfList[0], nil
	}
	return cqlList[0], nil
}

func (a *ZoomAvailabilityAdapter) HoldingsLookup(params LookupParams) ([]Holding, string, error) {
	var avail []Holding
	usedQuery, err := a.iterateQueries(params, func(xmlBuffer []byte) (bool, error) {
		h, err := a.holdingsParser.Parse(xmlBuffer, params)
		if err != nil {
			return false, fmt.Errorf("failed to parse holdings from Z39.50 record: %w", err)
		}
		avail = append(avail, h...)
		return true, nil
	}, func() bool {
		return len(avail) == 0
	})
	return avail, usedQuery, err
}

func (a *ZoomAvailabilityAdapter) MetadataLookup(params LookupParams) (Metadata, error) {
	var metadata Metadata
	if a.metadataParser == nil {
		return metadata, fmt.Errorf("metadata parser not configured")
	}
	found := false
	_, err := a.iterateQueries(params, func(xmlBuffer []byte) (bool, error) {
		parsed, err := a.metadataParser.Parse(xmlBuffer)
		if err != nil {
			return false, fmt.Errorf("failed to parse metadata from Z39.50 record: %w", err)
		}
		metadata = parsed
		found = true
		return false, nil // stop after first record
	}, func() bool {
		return !found
	})
	return metadata, err
}
