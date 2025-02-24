package adapter

import (
	"errors"
	"net/http"

	"github.com/indexdata/go-utils/utils"
)

func CreateHoldings() (HoldingsLookupAdapter, error) {
	holdingsType := utils.GetEnv("HOLDINGS_ADAPTER", "mock")
	if holdingsType == "sru" {
		adaptor := createSruHoldingsLookupAdapter(http.DefaultClient, utils.GetEnv("SRU_URL", "http://localhost:8081/sru"))
		return adaptor, nil
	}
	if holdingsType == "mock" {
		return &MockHoldingsLookupAdapter{}, nil
	}
	return nil, errors.New("bad value for HOLDINGS_ADAPTER")
}
