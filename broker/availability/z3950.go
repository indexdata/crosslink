// file: z3950.go
//go:build cgo

package availability

import (
	"encoding/json"
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/zoom"
)

func cgoEnabled() bool { return true }

type Z3950AvailabilityAdapter struct {
	options           zoom.Options
	zurl              string
	identifierMapping string
	isbnMapping       string
	issnMapping       string
	titleMapping      string
}

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config) (AvailabilityAdapter, error) {
	a := &Z3950AvailabilityAdapter{
		// default options, can be overridden by config.Options
		options: zoom.Options{
			"count":                 "10",
			"preferredRecordSyntax": "usmarc",
		},
		identifierMapping: "1=12",
		isbnMapping:       "1=7",
		issnMapping:       "1=8",
		titleMapping:      "1=4",

		zurl: config.Address,
	}
	if config.Options != nil {
		for k, v := range *config.Options {
			a.options[k] = v
		}
	}
	return a, nil
}

func (a *Z3950AvailabilityAdapter) searchRetrieve(conn *zoom.Connection, query string) ([]Availability, error) {
	res, err := conn.Search(query)
	if err != nil {
		return nil, err
	}
	var avail []Availability
	for i := 0; i < res.Count(); i++ {
		rec, err := res.GetRecord(i)
		if err != nil {
			return nil, err
		}
		jsonString := rec.Data("json;charset=utf-8")
		if jsonString == "" {
			continue
		}
		// parse jsonString to "any" type
		var jsonData map[string]any
		err = json.Unmarshal([]byte(jsonString), &jsonData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse JSON from Z39.50 record: %w", err)
		}
		avail = append(avail, Availability{
			Availability: jsonString,
		})
	}
	return avail, nil
}

func pqfEncode(value string) string {
	// escape backslashes and double quotes
	escaped := "\""
	for _, r := range value {
		if r == '\\' || r == '"' {
			escaped += "\\"
		}
		escaped += string(r)
	}
	escaped += "\""
	return escaped
}

func (a *Z3950AvailabilityAdapter) Lookup(params AvailabilityLookupParams) ([]Availability, error) {
	conn := zoom.NewConnection(a.options)
	defer conn.Close()
	err := conn.Connect(a.zurl)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Z39.50 server: %w", err)
	}
	type paramMapping struct {
		value   string
		mapping string
	}

	paramMappings := []paramMapping{
		{params.Identifier, a.identifierMapping},
		{params.Isbn, a.isbnMapping},
		{params.Issn, a.issnMapping},
		{params.Title, a.titleMapping},
	}
	for _, pm := range paramMappings {
		if pm.value != "" {
			avail, err := a.searchRetrieve(conn, "@attr "+pm.mapping+" "+pqfEncode(pm.value))
			if err != nil {
				return nil, fmt.Errorf("failed to search Z39.50 server: %w", err)
			}
			if len(avail) > 0 {
				return avail, nil
			}
		}
	}
	return nil, nil
}
