package availability

import "github.com/indexdata/crosslink/broker/adapter"

const (
	AvailabilityAdapterZoom string = "zoom" // yaz zoom adapter
	AvailabilityAdapterMock string = "mock" // mock adapter for testing
)

type AvailabilityAdapter interface {
	Lookup(params adapter.HoldingLookupParams) ([]adapter.Holding, error)
}
