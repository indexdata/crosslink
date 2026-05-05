package availability

const (
	AvailabilityAdapterZoom string = "zoom" // yaz zoom adapter
	AvailabilityAdapterMock string = "mock" // mock adapter for testing
)

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
	Location         string
	ShelvingLocation string
	CallNumber       string
	ItemId           string
}
