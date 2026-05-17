package app

import (
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestGetScenarioForRequest(t *testing.T) {
	request := &iso18626.Request{}
	request.BibliographicInfo.SupplierUniqueRecordId = "A"
	assert.Equal(t, "A", getScenarioForRequest(request, "SupplierUniqueRecordId"))
	assert.Equal(t, "UNFILLED", getScenarioForRequest(request, "Author"))
	assert.Equal(t, "UNFILLED", getScenarioForRequest(request, "Title"))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY"
	assert.Equal(t, "RETRY", getScenarioForRequest(request, ""))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY_"
	assert.Equal(t, "RETRY", getScenarioForRequest(request, ""))

	request.BibliographicInfo.SupplierUniqueRecordId = "RETRY:beta_LOANED"
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request, ""))

	request.ServiceInfo = &iso18626.ServiceInfo{ServiceType: "ILL"}
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request, ""))

	requestType := iso18626.TypeRequestTypeNew
	request.ServiceInfo.RequestType = &requestType
	assert.Equal(t, "RETRY:beta", getScenarioForRequest(request, ""))

	requestType = iso18626.TypeRequestTypeRetry
	request.ServiceInfo.RequestType = &requestType
	assert.Equal(t, "LOANED", getScenarioForRequest(request, ""))

	request.BibliographicInfo.Title = "LOANED_OVERDUE"
	request.BibliographicInfo.Author = "ERROR"

	assert.Equal(t, "LOANED_OVERDUE", getScenarioForRequest(request, "Title"))
	assert.Equal(t, "ERROR", getScenarioForRequest(request, "Author"))
}
