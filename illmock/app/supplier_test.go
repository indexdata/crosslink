package app

import (
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestGetScenarioForRequest(t *testing.T) {
	request := &iso18626.Request{}
	request.BibliographicInfo.SupplierUniqueRecordId = "A"
	assert.Equal(t, "A", getScenarioForRequest(request))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY"
	assert.Equal(t, "RETRY", getScenarioForRequest(request))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY_"
	assert.Equal(t, "RETRY", getScenarioForRequest(request))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY:beta_LOANED"
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request))

	request.ServiceInfo = &iso18626.ServiceInfo{ServiceType: "ILL"}
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request))

	requestType := iso18626.TypeRequestTypeNew
	request.ServiceInfo.RequestType = &requestType
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request))

	requestType = iso18626.TypeRequestTypeRetry
	request.ServiceInfo.RequestType = &requestType
	assert.Equal(t, "LOANED", getScenarioForRequest(request))
}

func TestGetScenarioNoteMissingComponent(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "MOCK:SYM1",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "SYM1"},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "A", getScenarioForRequest(request))
}

func TestGetScenarioEmpty(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "MOCK:SYM1: ",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "SYM1"},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "A", getScenarioForRequest(request))
}

func TestGetScenarioNoteSymMismatch(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "MOCK:SYM1:B",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "SYM2"},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "A", getScenarioForRequest(request))
}

func TestGetScenarioNoteMatchNoSuffix(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "xMOCK:SYM1:B",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "SYM1"},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "B", getScenarioForRequest(request))
}

func TestGetScenarioNoteMatchHash(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "xMOCK:SYM1:B#other note #",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "SYM1"},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "B", getScenarioForRequest(request))
}

func TestGetScenarioNoteMatchNewline(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "xMOCK:SYM1:B\n#other note #",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "SYM1"},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "B", getScenarioForRequest(request))
}

func TestGetScenarioNoteMatchSpace(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "xMOCK:SYM1:B other note",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "SYM1"},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "B", getScenarioForRequest(request))
}

func TestGetScenarioNoteEmptySym(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "xMOCK::B",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: ""},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "B", getScenarioForRequest(request))
}

func TestGetScenarioNoteMatchMultiple(t *testing.T) {
	request := &iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{
			Note: "MOCK:SYM1:C MOCK:SYM2\nMOCK:SYM2:B",
		},
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "SYM2"},
		},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "A"},
	}
	assert.Equal(t, "B", getScenarioForRequest(request))
}
