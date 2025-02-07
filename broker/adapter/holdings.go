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
	Symbol          string
	LocalIdentifier string
}

type MockHoldingsLookupAdapter struct {
}

func (m *MockHoldingsLookupAdapter) Lookup(params HoldingLookupParams) ([]Holding, error) {
	if strings.Index(params.Identifier, "return-") == 0 {
		value := strings.TrimPrefix(params.Identifier, "return-")
		return []Holding{{
			Symbol:          value,
			LocalIdentifier: value,
		}}, nil
	}
	if strings.Contains(params.Identifier, "error") {
		return []Holding{}, errors.New("there is error")
	}
	if strings.Contains(params.Identifier, "h-not-found") {
		return []Holding{}, nil
	}
	ids := strings.Split(params.Identifier, ",")
	i := 1
	var holdings []Holding
	for _, id := range ids {
		holdings = append(holdings, Holding{
			Symbol:          "isil:sup" + strconv.Itoa(i),
			LocalIdentifier: id,
		})
		i++
	}
	return holdings, nil
}
