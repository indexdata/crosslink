package holdings

import (
	"errors"
	"fmt"
	"strings"

	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/directory"
)

type QueryBuilderGen struct {
	config directory.QueryConfig
}

func NewString(s string) *string {
	if len(s) > 0 {
		return &s
	}
	return nil
}

func NewQueryBuilderGen(queryConfig *directory.QueryConfig) (LookupQueryBuilder, error) {
	var config directory.QueryConfig
	if queryConfig != nil {
		config = *queryConfig
	}
	if config.Type == nil || *config.Type == directory.Pqf {
		if config.Identifier == nil && config.Isbn == nil && config.Issn == nil && config.Title == nil {
			// if no specific mappings are provided, we set default PQF mappings
			config.Identifier = NewString("@attr 1=12 {term}")
			config.Isbn = NewString("@attr 1=7 {term}")
			config.Issn = NewString("@attr 1=8 {term}")
			config.Title = NewString("@attr 1=4 {term}")
		}
		return &QueryBuilderGen{config: config}, nil
	}
	if *config.Type == directory.Cql {
		if config.Identifier == nil && config.Isbn == nil && config.Issn == nil && config.Title == nil {
			// if no specific mappings are provided, we set default CQL mappings
			config.Identifier = NewString("rec.id = {term}")
			config.Isbn = NewString("isbn = {term}")
			config.Issn = NewString("issn = {term}")
			config.Title = NewString("title = {term}")
		}
		return &QueryBuilderGen{config: config}, nil
	}
	return nil, fmt.Errorf("unsupported query builder type: %s", *config.Type)
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
	return "\"" + cqlbuilder.EscapeMaskingChars(cqlbuilder.EscapeSpecialChars(value)) + "\""
}

func (s *QueryBuilderGen) Build(params LookupParams) (cql []string, pqf []string, err error) {
	type paramMapping struct {
		value   string
		mapping *string
	}

	paramMappings := []paramMapping{
		{params.Identifier, s.config.Identifier},
		{params.Isbn, s.config.Isbn},
		{params.Issn, s.config.Issn},
		{params.Title, s.config.Title},
	}
	var pqfList []string
	var cqlList []string
	for _, pm := range paramMappings {
		if pm.value != "" && pm.mapping != nil {
			if s.config.Type != nil && *s.config.Type == directory.Cql {
				cql := strings.ReplaceAll(*pm.mapping, "{term}", cqlEncode(pm.value))
				cqlList = append(cqlList, cql)
			} else {
				pqf := strings.ReplaceAll(*pm.mapping, "{term}", pqfEncode(pm.value))
				pqfList = append(pqfList, pqf)
			}
		}
	}
	if len(cqlList) == 0 && len(pqfList) == 0 {
		return nil, nil, errors.New("no search parameters provided for PQF/CQL query")
	}
	return cqlList, pqfList, nil
}
