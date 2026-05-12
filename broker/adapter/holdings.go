package adapter

type LookupAdapter interface {
	Lookup(params LookupParams) ([]Holding, string, error)
}

type LookupParams struct {
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

type LookupQueryBuilder interface {
	// Build should return the query strategy
	Build(params LookupParams) (cql []string, pqf []string, err error)
}
