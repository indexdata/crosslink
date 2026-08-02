package catalog

import (
	"fmt"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
)

type MockLookupAdapter struct {
	Err      error
	Holdings []Holding
	Metadata Metadata
}

type MockLookupResult struct {
	parent *MockLookupAdapter
}

func NewMockLookupAdapter(config dirapi.HoldingsConfig) (LookupAdapter, error) {
	if config.Zoom != nil && config.Zoom.Options != nil {
		options := *config.Zoom.Options
		// For testing purposes, we can use the presence of "adapter-error" in options to trigger an error response
		if val, ok := options["adapter-error"]; ok && strings.ToLower(val) == "true" {
			return nil, fmt.Errorf("mock error triggered by config")
		}
		// For testing purposes, we can use the presence of "lookup-error" in options to trigger an error response
		if val, ok := options["lookup-error"]; ok && strings.ToLower(val) == "true" {
			return &MockLookupAdapter{
				Err: fmt.Errorf("mock error triggered by config"),
			}, nil
		}
		if val, ok := options["location"]; ok {
			return &MockLookupAdapter{
				Holdings: []Holding{
					{
						Location: val,
					},
				},
			}, nil
		}
	}
	return &MockLookupAdapter{}, nil
}

func (a *MockLookupAdapter) Lookup(params LookupParams) (LookupResult, error) {
	if a.Err != nil {
		return nil, a.Err
	}
	return &MockLookupResult{parent: a}, nil
}

func (a *MockLookupResult) GetHoldings() ([]Holding, error) {
	return a.parent.Holdings, nil
}

func (a *MockLookupResult) GetMetadata() (Metadata, error) {
	return a.parent.Metadata, nil
}

func (a *MockLookupResult) GetQuery() string {
	return ""
}
