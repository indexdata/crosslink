package holdings

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type MockHoldingsLookupAdapter struct {
}

type MockHoldingsLookupResult struct {
	holdings    []Holding
	holdingsErr error
	query       string
}

// the original mock holdings adapter that we used for shared index testing
func (m *MockHoldingsLookupAdapter) Lookup(params LookupParams) (LookupResult, error) {
	var result MockHoldingsLookupResult
	result.holdings = []Holding{}
	result.query = params.Identifier
	ids := strings.Split(params.Identifier, ";")
	i := 1
	for _, id := range ids {
		if id == "" {
			// if the identifier is empty, we return an error to simulate a missing parameter
			return &result, errors.New("missing lookup parameter: identifier")
		}
		// LookupResult should return an error if the identifier is "error"
		if id == "error" {
			return &result, errors.New("there is error")
		}
		if id == "error-holdings" {
			result.holdingsErr = errors.New("there is error in holdings")
		}
		if id == "not-found" { // we could also just not append?
			return &result, nil
		}
		if strings.Index(id, "return-") == 0 {
			val := strings.SplitN(strings.TrimPrefix(id, "return-"), "::", 2)
			if len(val) < 1 || len(val[0]) < 1 {
				return &result, fmt.Errorf("invalid return- value")
			}
			var s, l string
			if len(val) == 1 {
				s = val[0]
				l = val[0]
			}
			if len(val) == 2 {
				s = val[0]
				l = val[1]
			}
			result.holdings = append(result.holdings, Holding{
				Symbol:          s,
				LocalIdentifier: l,
			})
		} else {
			result.holdings = append(result.holdings, Holding{
				Symbol:          "ISIL:SUP" + strconv.Itoa(i),
				LocalIdentifier: id,
			})
		}
		i++
	}
	return &result, nil
}

func (m *MockHoldingsLookupResult) GetMetadata() (Metadata, error) {
	var metadata Metadata
	metadata.Identifier = m.query
	return metadata, nil
}

func (m *MockHoldingsLookupResult) GetHoldings() ([]Holding, error) {
	if m.holdingsErr != nil {
		return nil, m.holdingsErr
	}
	return m.holdings, nil
}

func (m *MockHoldingsLookupResult) GetQuery() string {
	return m.query
}
