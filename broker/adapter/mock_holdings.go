package adapter

import (
	"errors"
	"fmt"
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
		if id == "not-found" { // we could also just not append?
			return []Holding{}, nil
		}
		if strings.Index(id, "return-") == 0 {
			val := strings.SplitN(strings.TrimPrefix(id, "return-"), "_", 2)
			if len(val) < 1 || len(val[0]) < 1 {
				return nil, fmt.Errorf("invalid return- value")
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
			holdings = append(holdings, Holding{
				Symbol:          s,
				LocalIdentifier: l,
			})
		} else {
			holdings = append(holdings, Holding{
				Symbol:          "ISIL:SUP" + strconv.Itoa(i),
				LocalIdentifier: id,
			})
		}
		i++
	}
	return holdings, nil
}
