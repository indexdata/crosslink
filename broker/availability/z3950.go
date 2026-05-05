// file: z3950.go
//go:build cgo

package availability

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/zoom"
)

func cgoEnabled() bool { return true }

type Z3950AvailabilityAdapter struct {
	zurl        string
	options     zoom.Options
	pqfMappings directory.PqfMappings
}

func NewZ3950AvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config) (AvailabilityAdapter, error) {
	a := &Z3950AvailabilityAdapter{
		// default options, can be overridden by config.Options
		options: zoom.Options{
			"count":                 "10",
			"preferredRecordSyntax": "usmarc",
		},
		zurl: config.Address,
	}
	if config.Options != nil {
		for k, v := range *config.Options {
			a.options[k] = v
		}
	}
	if config.PqfMappings != nil {
		a.pqfMappings = *config.PqfMappings
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
		if rec == nil {
			continue
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
			Location: jsonString,
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
		mapping *string
		dir     string
	}

	paramMappings := []paramMapping{
		{params.Identifier, a.pqfMappings.Identifier, "@attr 1=12 {term}"},
		{params.Isbn, a.pqfMappings.Isbn, "@attr 1=7 {term}"},
		{params.Issn, a.pqfMappings.Issn, "@attr 1=8 {term}"},
		{params.Title, a.pqfMappings.Title, "@attr 1=4 {term}"},
	}
	for _, pm := range paramMappings {
		if pm.value != "" {
			mapping := pm.dir
			if pm.mapping != nil {
				mapping = *pm.mapping
			}
			pqf := strings.ReplaceAll(mapping, "{term}", pqfEncode(pm.value))
			avail, err := a.searchRetrieve(conn, pqf)
			if err != nil {
				return nil, fmt.Errorf("failed to search Z39.50 server query: %s err %w", pqf, err)
			}
			if len(avail) > 0 {
				return avail, nil
			}
		}
	}
	return nil, nil
}
