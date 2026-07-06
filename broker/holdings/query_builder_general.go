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
		if config.Identifier == nil {
			config.Identifier = NewString("@attr 1=12 {term}")
		}
		if config.Isbn == nil {
			config.Isbn = NewString("@attr 1=7 {term}")
		}
		if config.Issn == nil {
			config.Issn = NewString("@attr 1=8 {term}")
		}
		if config.Title == nil {
			config.Title = NewString("@attr 1=4 {term}")
		}
		return &QueryBuilderGen{config: config}, nil
	}
	if *config.Type == directory.Cql {
		if config.Identifier == nil {
			config.Identifier = NewString("rec.id = {term}")
		}
		if config.Isbn == nil {
			config.Isbn = NewString("isbn = {term}")
		}
		if config.Issn == nil {
			config.Issn = NewString("issn = {term}")
		}
		if config.Title == nil {
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
		name    string
	}

	paramMappings := []paramMapping{
		{params.Identifier, s.config.Identifier, "identifier"},
		{params.Isbn, s.config.Isbn, "isbn"},
		{params.Issn, s.config.Issn, "issn"},
		{params.Title, s.config.Title, "title"},
	}
	var pqfList []string
	var cqlList []string
	for _, pm := range paramMappings {
		if pm.value != "" && pm.mapping != nil && *pm.mapping != "" {
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
		var allowedLookupIdentifiers []string
		for _, pm := range paramMappings {
			if pm.mapping != nil && *pm.mapping != "" {
				allowedLookupIdentifiers = append(allowedLookupIdentifiers, pm.name)
			}
		}
		return nil, nil, errors.New("missing lookup parameters. Provide at least one of: " + strings.Join(allowedLookupIdentifiers, ", "))
	}
	return cqlList, pqfList, nil
}
