package availability

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/directory"
)

type MockAvailabilityAdapter struct {
	Err      error
	Holdings []adapter.Holding
}

func NewMockAvailabilityAdapter(config directory.AvailabilityConfig) (adapter.LookupAdapter, error) {
	if config.Z3950 != nil && config.Z3950.Options != nil {
		options := *config.Z3950.Options
		// For testing purposes, we can use the presence of "adapter-error" in options to trigger an error response
		if val, ok := options["adapter-error"]; ok && strings.ToLower(val) == "true" {
			return nil, fmt.Errorf("mock error triggered by config")
		}
		// For testing purposes, we can use the presence of "lookup-error" in options to trigger an error response
		if val, ok := options["lookup-error"]; ok && strings.ToLower(val) == "true" {
			return &MockAvailabilityAdapter{
				Err: fmt.Errorf("mock error triggered by config"),
			}, nil
		}
		if val, ok := options["location"]; ok {
			return &MockAvailabilityAdapter{
				Holdings: []adapter.Holding{
					{
						Location: val,
					},
				},
			}, nil
		}
	}
	return &MockAvailabilityAdapter{}, nil
}

func (a *MockAvailabilityAdapter) Lookup(params adapter.LookupParams) ([]adapter.Holding, string, error) {
	if a.Err != nil {
		return nil, "", a.Err
	}
	return a.Holdings, "", nil
}
