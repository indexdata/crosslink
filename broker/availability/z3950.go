// file: z3950.go
//go:build cgo

package availability

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/zoom"
)

func cgoEnabled() bool { return true }

type Z3950AvailabilityAdapter struct {
	zurl           string
	options        zoom.Options
	holdingsParser adapter.HoldingsParser
	queryBuilder   adapter.HoldingsQueryBuilder
}

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config) (adapter.HoldingsLookupAdapter, error) {
	// TODO: holdingsParser based on config
	a := &Z3950AvailabilityAdapter{
		// default options, can be overridden by config.Options
		options: zoom.Options{
			"count":                 "10",
			"preferredRecordSyntax": "usmarc",
		},
		zurl:           config.Address,
		holdingsParser: adapter.NewMarcHoldingsParser(nil),
	}
	if config.Options != nil {
		for k, v := range *config.Options {
			a.options[k] = v
		}
	}
	a.queryBuilder = adapter.NewQueryBuilderPqf(config.PqfMappings)
	return a, nil
}

func (a *Z3950AvailabilityAdapter) searchRetrieve(conn *zoom.Connection, query string) ([]adapter.Holding, error) {
	res, err := conn.Search(query)
	if err != nil {
		return nil, err
	}
	var avail []adapter.Holding
	for i := 0; i < res.Count(); i++ {
		rec, err := res.GetRecord(i)
		if err != nil {
			return nil, err
		}
		if rec == nil {
			continue
		}
		xmlBuffer := rec.Data("xml;charset=utf-8")
		if xmlBuffer == nil {
			continue
		}
		holdings, err := a.holdingsParser.Parse(xmlBuffer)
		if err != nil {
			return nil, fmt.Errorf("failed to parse holdings from Z39.50 record: %w", err)
		}
		avail = append(avail, holdings...)
	}
	return avail, nil
}

func (a *Z3950AvailabilityAdapter) Lookup(params adapter.HoldingLookupParams) ([]adapter.Holding, string, error) {
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
	if len(cqlList) > 0 {
		return nil, "", fmt.Errorf("Z39.50 server does not support CQL queries: %v", cqlList)
	}
	if len(pqfList) == 0 {
		return nil, "", fmt.Errorf("no valid query parameters provided")
	}
	for _, pqf := range pqfList {
		avail, err := a.searchRetrieve(conn, pqf)
		if err != nil {
			return nil, "", fmt.Errorf("failed to search Z39.50 server query: %s err %w", pqf, err)
		}
		if len(avail) > 0 {
			return avail, pqf, nil
		}
	}
	return nil, pqfList[0], nil
}
