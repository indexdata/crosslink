package adapter

import (
	"strings"

	"github.com/indexdata/crosslink/directory"
)

type QueryBuilderPqf struct {
	pqfMappings directory.PqfMappings
	defaultMap  directory.PqfMappings
}

func NewString(s string) *string {
	if len(s) > 0 {
		return &s
	}
	return nil
}

func NewQueryBuilderPqf(pqfMappings *directory.PqfMappings) *QueryBuilderPqf {
	if pqfMappings == nil {
		pqfMappings = &directory.PqfMappings{}
	}
	return &QueryBuilderPqf{pqfMappings: *pqfMappings, defaultMap: directory.PqfMappings{
		Identifier: NewString("@attr 1=12 {term}"),
		Isbn:       NewString("@attr 1=7 {term}"),
		Issn:       NewString("@attr 1=8 {term}"),
		Title:      NewString("@attr 1=4 {term}"),
	}}
}

func NewQueryBuilderCql(pqfMappings *directory.PqfMappings) *QueryBuilderPqf {
	if pqfMappings == nil {
		pqfMappings = &directory.PqfMappings{}
	}
	return &QueryBuilderPqf{pqfMappings: *pqfMappings, defaultMap: directory.PqfMappings{
		Identifier: NewString("rec.id = {term}"),
		Isbn:       NewString("isbn = {term}"),
		Issn:       NewString("issn = {term}"),
		Title:      NewString("title = {term}"),
	}}
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

func cqlEncode(value string) string {
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

func (s *QueryBuilderPqf) Build(params HoldingLookupParams) (cql []string, pqf []string, err error) {
	type paramMapping struct {
		value   string
		mapping *string
		dir     string
	}

	paramMappings := []paramMapping{
		{params.Identifier, s.pqfMappings.Identifier, *s.defaultMap.Identifier},
		{params.Isbn, s.pqfMappings.Isbn, *s.defaultMap.Isbn},
		{params.Issn, s.pqfMappings.Issn, *s.defaultMap.Issn},
		{params.Title, s.pqfMappings.Title, *s.defaultMap.Title},
	}
	var pqfList []string
	var cqlList []string
	for _, pm := range paramMappings {
		if pm.value != "" {
			mapping := pm.dir
			if pm.mapping != nil {
				mapping = *pm.mapping
			}
			if strings.HasPrefix(mapping, "@") {
				pqf := strings.ReplaceAll(mapping, "{term}", pqfEncode(pm.value))
				pqfList = append(pqfList, pqf)
			} else {
				cql := strings.ReplaceAll(mapping, "{term}", cqlEncode(pm.value))
				cqlList = append(cqlList, cql)
			}
		}
	}
	return cqlList, pqfList, nil
}
