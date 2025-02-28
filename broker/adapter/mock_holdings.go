package adapter

import (
	"errors"
	"strconv"
	"strings"
)

type MockHoldingsLookupAdapter struct {
}

func (m *MockHoldingsLookupAdapter) Lookup(params HoldingLookupParams) ([]Holding, error) {
	ids := strings.Split(params.Identifier, ";")
	i := 1
	var holdings []Holding
	for _, id := range ids {
		if id == "error" {
			return []Holding{}, errors.New("there is error")
		}
		// is it "h-not-found" or "not-found"?
		if id == "h-not-found" { // we could also just not append?
			return []Holding{}, nil
		}
		if strings.Index(id, "return-") == 0 {
			value := strings.TrimPrefix(id, "return-")
			holdings = append(holdings, Holding{
				Symbol:          value,
				LocalIdentifier: value,
			})
		} else {
			holdings = append(holdings, Holding{
				Symbol:          "isil:sup" + strconv.Itoa(i),
				LocalIdentifier: id,
			})
		}
		i++
	}
	return holdings, nil
}
