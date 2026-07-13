package catalog

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/directory"
)

type MockAvailabilityAdapter struct {
	Err      error
	Holdings []Holding
	Metadata Metadata
}

type MockAvailabilityLookupResult struct {
	parent *MockAvailabilityAdapter
}

func NewMockAvailabilityAdapter(config directory.CatalogConfig) (LookupAdapter, error) {
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

func (a *MockAvailabilityAdapter) Lookup(params LookupParams) (LookupResult, error) {
	if a.Err != nil {
		return nil, a.Err
	}
	return &MockAvailabilityLookupResult{parent: a}, nil
}

func (a *MockAvailabilityLookupResult) GetHoldings() ([]Holding, error) {
	return a.parent.Holdings, nil
}

func (a *MockAvailabilityLookupResult) GetMetadata() (Metadata, error) {
	return a.parent.Metadata, nil
}

func (a *MockAvailabilityLookupResult) GetQuery() string {
	return ""
}
