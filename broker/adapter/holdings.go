package adapter

import (
	"errors"
	"strconv"
	"strings"
)

type HoldingsLookupAdapter interface {
	Lookup(params HoldingLookupParams) ([]Holding, error)
}

type HoldingLookupParams struct {
	Identifier string
}

type Holding struct {
	Symbol     string
	Identifier string
}

type MockHoldingsLookupAdapter struct {
}

func (m *MockHoldingsLookupAdapter) Lookup(params HoldingLookupParams) ([]Holding, error) {
	if strings.Contains(params.Identifier, "error") {
		return []Holding{}, errors.New("there is error")
	}
	if strings.Contains(params.Identifier, "h-not-found") {
		return []Holding{}, nil
	}
	if strings.Index(params.Identifier, "return-") == 0 {
		value := strings.TrimPrefix(params.Identifier, "return-")
		return []Holding{{
			Symbol:     value,
			Identifier: value,
		}}, nil
	}
	count := 1
	if strings.Index(params.Identifier, "count-") == 0 {
		value := strings.TrimPrefix(params.Identifier, "count-")
		num, err := strconv.Atoi(value)
		if err == nil {
			count = num
		}
	}
	var holdings []Holding
	for i := 1; i <= count; i++ {
		holdings = append(holdings, Holding{
			Symbol:     "isil:resp" + strconv.Itoa(i),
			Identifier: "658a98ab-866e-48f7-b2eb-da5f95ca525" + strconv.Itoa(i),
		})
	}
	return holdings, nil
}
