package availability

type AvailabilityAdapter interface {
	Lookup(params AvailabilityLookupParams) ([]Availability, error)
}

type AvailabilityLookupParams struct {
	Identifier string
	Isbn       string
	Issn       string
	Title      string
}

type Availability struct {
	Availability string
}
