package app

import (
	"testing"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestGetScenarioForRequest(t *testing.T) {
	request := &iso18626.Request{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "LIB1",
			},
		},
		BibliographicInfo: iso18626.BibliographicInfo{},
	}
	request.BibliographicInfo.SupplierUniqueRecordId = "A"
	assert.Equal(t, "A", getScenarioForRequest(request, nil))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY"
	assert.Equal(t, "RETRY", getScenarioForRequest(request, nil))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY_"
	assert.Equal(t, "RETRY", getScenarioForRequest(request, nil))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY:beta_LOANED"
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request, nil))

	request.ServiceInfo = &iso18626.ServiceInfo{ServiceType: "ILL"}
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request, nil))

	requestType := iso18626.TypeRequestTypeNew
	request.ServiceInfo.RequestType = &requestType
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request, nil))

	requestType = iso18626.TypeRequestTypeRetry
	request.ServiceInfo.RequestType = &requestType
	assert.Equal(t, "LOANED", getScenarioForRequest(request, nil))

	requestType = iso18626.TypeRequestTypeNew
	request.BibliographicInfo.SupplierUniqueRecordId = "LOANED"
	request.ServiceInfo.RequestType = &requestType

}

func TestGetScenarioForRequestWithDirectory(t *testing.T) {
	request := &iso18626.Request{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "LIB1",
			},
		},
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "LOANED",
		},
	}

	var description string
	description = "lib"
	directoryEntries := []directory.Entry{
		{
			Description: &description,
		},
	}
	assert.Equal(t, "LOANED", getScenarioForRequest(request, directoryEntries))

	directoryEntries = append(directoryEntries, directory.Entry{
		Symbols: &[]directory.Symbol{
			{
				Authority: "ISIL",
				Symbol:    "LIB0",
			},
		},
	})
	directoryEntries = append(directoryEntries, directory.Entry{
		Symbols: &[]directory.Symbol{
			{
				Authority: "ISIL",
				Symbol:    "LIB1",
			},
		},
	})
	assert.Equal(t, "LOANED", getScenarioForRequest(request, directoryEntries))

	directoryEntries = append(directoryEntries, directory.Entry{
		Description: &description,
		Symbols: &[]directory.Symbol{
			{
				Authority: "ISIL",
				Symbol:    "LIB1",
			},
		},
	})
	assert.Equal(t, "LOANED", getScenarioForRequest(request, directoryEntries))
	description = "lib MOCK:WILLSUPPLY"
	directoryEntries = append(directoryEntries, directory.Entry{
		Description: &description,
		Symbols: &[]directory.Symbol{
			{
				Authority: "ISIL",
				Symbol:    "LIB1",
			},
		},
	})
	assert.Equal(t, "WILLSUPPLY", getScenarioForRequest(request, directoryEntries))
}
