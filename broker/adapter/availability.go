package adapter

type AvailabilityAdapter interface {
	Lookup(params AvailabilityLookupParams) ([]Availability, error)
}

type AvailabilityLookupParams struct {
	Identifier string
}

type Availability struct {
	Availability string
}
