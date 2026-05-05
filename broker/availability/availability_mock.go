package availability

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/directory"
)

type MockAvailabilityAdapter struct {
	Err          error
	Availability []Availability
}

func NewMockAvailabilityAdapter(config directory.Z3950Config) (AvailabilityAdapter, error) {
	if config.Options != nil {
		// For testing purposes, we can use the presence of "adapter-error" in options to trigger an error response
		if val, ok := (*config.Options)["adapter-error"]; ok && strings.ToLower(val) == "true" {
			return nil, fmt.Errorf("mock error triggered by config")
		}
		// For testing purposes, we can use the presence of "lookup-error" in options to trigger an error response
		if val, ok := (*config.Options)["lookup-error"]; ok && strings.ToLower(val) == "true" {
			return &MockAvailabilityAdapter{
				Err: fmt.Errorf("mock error triggered by config"),
			}, nil
		}
		if val, ok := (*config.Options)["location"]; ok {
			return &MockAvailabilityAdapter{
				Availability: []Availability{
					{
						Location: val,
					},
				},
			}, nil
		}
	}
	return &MockAvailabilityAdapter{}, nil
}

func (a *MockAvailabilityAdapter) Lookup(params AvailabilityLookupParams) ([]Availability, error) {
	if a.Err != nil {
		return nil, a.Err
	}
	return a.Availability, nil
}
