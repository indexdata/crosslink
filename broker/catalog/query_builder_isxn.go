package catalog

import (
	"errors"
	"strings"
)

type QueryBuilderIsxn struct {
	isxn bool
}

func NewQueryBuilderIsxn(isxn bool) LookupQueryBuilder {
	return &QueryBuilderIsxn{isxn: isxn}
}

func (s *QueryBuilderIsxn) Build(params LookupParams) (cql []string, pqf []string, err error) {
	var comps []string
	if params.Identifier != "" {
		cql, err := encodeCqlSearchClause("rec.id", params.Identifier)
		if err != nil {
			return nil, nil, err
		}
		comps = append(comps, cql)
	}
	if s.isxn && params.Isbn != "" {
		cql, err := encodeCqlSearchClause("isbn", params.Isbn)
		if err != nil {
			return nil, nil, err
		}
		comps = append(comps, cql)
	}
	if s.isxn && params.Issn != "" {
		cql, err := encodeCqlSearchClause("issn", params.Issn)
		if err != nil {
			return nil, nil, err
		}
		comps = append(comps, cql)
	}
	if len(comps) == 0 {
		allowedLookupIdentifiers := []string{"identifier (supplierUniqueRecordId)"}
		if s.isxn {
			allowedLookupIdentifiers = append(allowedLookupIdentifiers, "isbn", "issn")
		}
		return nil, nil, errors.New("missing SRU lookup parameters. Provide at least one of: " + strings.Join(allowedLookupIdentifiers, ", "))
	}
	// combine components with OR. Just one query returned since we want to search for all provided identifiers at once
	return []string{strings.Join(comps, " or ")}, nil, nil
}
