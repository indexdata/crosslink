package availability

type AvailabilityAdapter interface {
	Lookup(params AvailabilityLookupParams) ([]Availability, error)
}

type AvailabilityLookupParams struct {
	Identifier string
}

type Availability struct {
	Availability string
}
