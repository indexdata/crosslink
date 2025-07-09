package adapter

type HoldingsLookupAdapter interface {
	Lookup(params HoldingLookupParams) ([]Holding, string, error)
}

type HoldingLookupParams struct {
	Identifier string
}

type Holding struct {
	Symbol          string
	LocalIdentifier string
}
