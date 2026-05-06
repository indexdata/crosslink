package adapter

type HoldingsLookupAdapter interface {
	Lookup(params HoldingLookupParams) ([]Holding, string, error)
}

type HoldingLookupParams struct {
	Identifier string
	Isbn       string
	Issn       string
	Title      string
}

type Holding struct {
	Symbol           string
	LocalIdentifier  string
	Location         string
	ShelvingLocation string
	CallNumber       string
	ItemId           string
}

type HoldingsParser interface {
	Parse(record []byte) ([]Holding, error)
}

type HoldingsQueryBuilder interface {
	// Build should return the query
	Build(params HoldingLookupParams) (cql string, pqf string, err error)
}
