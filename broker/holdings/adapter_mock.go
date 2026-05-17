package holdings

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/directory"
)

type MockAvailabilityAdapter struct {
	Err      error
	Holdings []Holding
}

func NewMockAvailabilityAdapter(config directory.HoldingsConfig) (LookupAdapter, error) {
	if config.Zoom != nil && config.Zoom.Options != nil {
		options := *config.Zoom.Options
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
				Holdings: []Holding{
					{
						Location: val,
					},
				},
			}, nil
		}
	}
	return &MockAvailabilityAdapter{}, nil
}

func (a *MockAvailabilityAdapter) Lookup(params LookupParams) ([]Holding, string, error) {
	if a.Err != nil {
		return nil, "", a.Err
	}
	return a.Holdings, "", nil
}
