package adapter

import (
	"errors"
	"net/http"
)

func CreateHoldings(holdingsType string, sruUrl string) (HoldingsLookupAdapter, error) {
	if holdingsType == "sru" {
		adaptor := CreateSruHoldingsLookupAdapter(http.DefaultClient, sruUrl)
		return adaptor, nil
	}
	if holdingsType == "mock" {
		return &MockHoldingsLookupAdapter{}, nil
	}
	return nil, errors.New("bad value for HOLDINGS_ADAPTER")
}
