package catalog

import "errors"

// ErrMissingLookupParameters identifies a lookup that cannot be performed
// because none of the parameters supported by its query builder are present.
var ErrMissingLookupParameters = errors.New("missing lookup parameters")

type missingLookupParametersError struct {
	message string
}

func (e *missingLookupParametersError) Error() string {
	return e.message
}

func (e *missingLookupParametersError) Unwrap() error {
	return ErrMissingLookupParameters
}

func newMissingLookupParametersError(message string) error {
	return &missingLookupParametersError{message: message}
}

type LookupAdapter interface {
	Lookup(params LookupParams) (LookupResult, error)
}

type LookupResult interface {
	GetQuery() string
	GetHoldings() ([]Holding, error)
	GetMetadata() (Metadata, error)
}

type LookupParams struct {
	Identifier  string
	Isbn        string
	Issn        string
	Title       string
	ServiceType string
}

type Holding struct {
	Symbol           string
	LocalIdentifier  string
	Location         string
	ShelvingLocation string
	ItemLoanPolicy   string
	CallNumber       string
	ItemId           string
}

type HoldingsParser interface {
	Parse(record []byte, params LookupParams) ([]Holding, error)
}

type LookupQueryBuilder interface {
	// Build should return the query strategy
	Build(params LookupParams) (cql []string, pqf []string, err error)
}

type MetadataParser interface {
	Parse(record []byte) (Metadata, error)
}

type Metadata struct {
	Identifier string
	Title      string
	Subtitle   string
	Author     string
	Edition    string
	Isbn       string
	Issn       string
}
