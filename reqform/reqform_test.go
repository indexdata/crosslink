package app

import (
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestFormBindingWithTags(t *testing.T) {
	// Example nested struct
	type Address struct {
		Street string `form:"address.street"`
		City   string `form:"address.city"`
	}

	// Example struct with nested struct
	type User struct {
		Name    string `form:"user.name"`
		Pass    string `form:"user.pass"`
		Address Address
	}

	// Example form data
	form := map[string][]string{
		"user.name":      {"JohnDoe"},
		"user.pass":      {"secret"},
		"address.street": {"123 Main St"},
		"address.city":   {"Anytown"},
	}

	// Create an instance of the struct
	user := &User{}

	// Populate the struct with form data
	err := BindFormWithTags(user, form)
	if err != nil {
		assert.Nil(t, err)
	}

	// Output the populated struct
	assert.Equal(t, "JohnDoe", user.Name)
	assert.Equal(t, "secret", user.Pass)
	assert.Equal(t, "123 Main St", user.Address.Street)
	assert.Equal(t, "Anytown", user.Address.City)
}

func TestFormBindingNoTags(t *testing.T) {
	// Example nested struct
	type Address struct {
		Street string
		Number int
		City   string
	}

	// Example struct with nested struct
	type User struct {
		Name    string
		Pass    string
		Address Address
	}

	// Example form data
	form := map[string][]string{
		"Name":           {"JohnDoe"},
		"Pass":           {"secret"},
		"Address.Street": {"Main St"},
		"Address.Number": {"123"},
		"Address.City":   {"Anytown"},
	}

	// Create an instance of the struct
	user := &User{}

	// Populate the struct with form data
	errs := BindForm(user, form)
	assert.Empty(t, errs)

	// Output the populated struct
	assert.Equal(t, "JohnDoe", user.Name)
	assert.Equal(t, "secret", user.Pass)
	assert.Equal(t, "Main St", user.Address.Street)
	assert.Equal(t, 123, user.Address.Number)
	assert.Equal(t, "Anytown", user.Address.City)
}

func TestBindForm(t *testing.T) {
	form := map[string][]string{
		"Header.SupplyingAgencyIdType":                        {"Type1"},
		"Header.SupplyingAgencyIdValue":                       {"Value1"},
		"Header.RequestingAgencyIdType":                       {"Type2"},
		"Header.RequestingAgencyIdValue":                      {"Value2"},
		"Header.MultipleItemRequestId":                        {"RequestId1"},
		"Header.Timestamp":                                    {"2025-02-24T10:00:00"},
		"Header.RequestingAgencyRequestId":                    {"RequestId2"},
		"BibliographicInfo.SupplierUniqueRecordId":            {"RecordId1"},
		"BibliographicInfo.Title":                             {"Title1"},
		"BibliographicInfo.Author":                            {"Author1"},
		"BibliographicInfo.Subtitle":                          {"Subtitle1"},
		"BibliographicInfo.SeriesTitle":                       {"SeriesTitle1"},
		"BibliographicInfo.Edition":                           {"Edition1"},
		"BibliographicInfo.TitleOfComponent":                  {"ComponentTitle1"},
		"BibliographicInfo.AuthorOfComponent":                 {"ComponentAuthor1"},
		"BibliographicInfo.Volume":                            {"Volume1"},
		"BibliographicInfo.Issue":                             {"Issue1"},
		"BibliographicInfo.PagesRequested":                    {"Pages1"},
		"BibliographicInfo.EstimatedNoPages":                  {"100"},
		"BibliographicInfo.BibliographicItemIdentifier":       {"Identifier1"},
		"BibliographicInfo.BibliographicItemIdentifierCode":   {"Code1"},
		"BibliographicInfo.Sponsor":                           {"Sponsor1"},
		"BibliographicInfo.InformationSource":                 {"Source1"},
		"BibliographicInfo.BibliographicRecordIdentifierCode": {"RecordCode1"},
		"BibliographicInfo.BibliographicRecordIdentifier":     {"RecordIdentifier1"},
		"PublicationInfo.Publisher":                           {"Publisher1"},
		"PublicationInfo.PublicationType":                     {"Type1"},
		"PublicationInfo.PublicationDate":                     {"2025-02-24"},
		"PublicationInfo.PlaceOfPublication":                  {"Place1"},
		"ServiceInfo.RequestType":                             {"New"},
		"ServiceInfo.RequestSubType":                          {"BookingRequest"},
		"ServiceInfo.RequestingAgencyPreviousRequestId":       {"PreviousRequestId1"},
		"ServiceInfo.ServiceType":                             {"Copy"},
		"ServiceInfo.ServiceLevel":                            {"Level1"},
		"ServiceInfo.PreferredFormat":                         {"Format1"},
		"ServiceInfo.NeedBeforeDate":                          {"2025-03-01"},
		"ServiceInfo.CopyrightCompliance":                     {"Compliance1"},
		"ServiceInfo.AnyEdition":                              {"Y"},
		"ServiceInfo.StartDate":                               {"2025-02-25"},
		"ServiceInfo.EndDate":                                 {"2025-03-01"},
		"ServiceInfo.Note":                                    {"Note1"},
		"SupplierInfo.SortOrder":                              {"1"},
		"SupplierInfo.SupplierCode":                           {"Code1"},
		"SupplierInfo.SupplierDescription":                    {"Description1"},
		"SupplierInfo.CallNumber":                             {"CallNumber1"},
		"SupplierInfo.SummaryHoldings":                        {"Holdings1"},
		"SupplierInfo.AvailabilityNote":                       {"Note1"},
		"RequestedDeliveryInfo.SortOrder":                     {"1"},
		"RequestedDeliveryInfo.Address":                       {"Address1"},
		"RequestingAgencyInfo.Name":                           {"AgencyName1"},
		"RequestingAgencyInfo.ContactName":                    {"ContactName1"},
		"RequestingAgencyInfo.Address":                        {"AgencyAddress1"},
		"PatronInfo.PatronId":                                 {"PatronId1"},
		"PatronInfo.Surname":                                  {"Surname1"},
		"PatronInfo.GivenName":                                {"GivenName1"},
		"PatronInfo.PatronType":                               {"Type1"},
		"PatronInfo.SendToPatron":                             {"Y"},
		"PatronInfo.Address":                                  {"PatronAddress1"},
		"BillingInfo.PaymentMethod":                           {"Method1"},
		"BillingInfo.MaximumCosts":                            {"1000"},
		"BillingInfo.BillingMethod":                           {"Method2"},
		"BillingInfo.BillingName":                             {"BillingName1"},
		"BillingInfo.Address.ElectronicAddressType":           {"Email"},
		"BillingInfo.Address.ElectronicAddressData":           {"email@example.com"},
		"BillingInfo.Address.Line1":                           {"Line1"},
		"BillingInfo.Address.Line2":                           {"Line2"},
		"BillingInfo.Address.Locality":                        {"Locality1"},
		"BillingInfo.Address.PostalCode":                      {"12345"},
		"BillingInfo.Address.Region":                          {"Region1"},
		"BillingInfo.Address.Country":                         {"Country1"},
	}

	formData := &iso18626.Request{}
	errors := BindForm(formData, form)
	if len(errors) > 0 {
		t.Fatalf("BindForm returned errors: %v", errors)
	}

	// Add assertions to verify that the form data was correctly set
	if formData.Header.SupplyingAgencyId.AgencyIdType != "Type1" {
		t.Errorf("Expected Header.SupplyingAgencyIdType to be 'Type1', got '%s'", formData.Header.SupplyingAgencyIdType)
	}
	if formData.Header.SupplyingAgencyId.AgencyIdValue != "Value1" {
		t.Errorf("Expected Header.SupplyingAgencyIdValue to be 'Value1', got '%s'", formData.Header.SupplyingAgencyIdValue)
	}
	if formData.Header.RequestingAgencyId.AgencyIdType != "Type2" {
		t.Errorf("Expected Header.RequestingAgencyIdType to be 'Type2', got '%s'", formData.Header.RequestingAgencyIdType)
	}
	if formData.Header.RequestingAgencyId.AgencyIdValue != "Value2" {
		t.Errorf("Expected Header.RequestingAgencyIdValue to be 'Value2', got '%s'", formData.Header.RequestingAgencyIdValue)
	}
	if formData.Header.MultipleItemRequestId != "RequestId1" {
		t.Errorf("Expected Header.MultipleItemRequestId to be 'RequestId1', got '%s'", formData.Header.MultipleItemRequestId)
	}
	if formData.Header.Timestamp != "2025-02-24T10:00:00" {
		t.Errorf("Expected Header.Timestamp to be '2025-02-24T10:00:00', got '%s'", formData.Header.Timestamp)
	}
	if formData.Header.RequestingAgencyRequestId != "RequestId2" {
		t.Errorf("Expected Header.RequestingAgencyRequestId to be 'RequestId2', got '%s'", formData.Header.RequestingAgencyRequestId)
	}
	if formData.BibliographicInfo.SupplierUniqueRecordId != "RecordId1" {
		t.Errorf("Expected BibliographicInfo.SupplierUniqueRecordId to be 'RecordId1', got '%s'", formData.BibliographicInfo.SupplierUniqueRecordId)
	}
	if formData.BibliographicInfo.Title != "Title1" {
		t.Errorf("Expected BibliographicInfo.Title to be 'Title1', got '%s'", formData.BibliographicInfo.Title)
	}
	if formData.BibliographicInfo.Author != "Author1" {
		t.Errorf("Expected BibliographicInfo.Author to be 'Author1', got '%s'", formData.BibliographicInfo.Author)
	}
	if formData.BibliographicInfo.Subtitle != "Subtitle1" {
		t.Errorf("Expected BibliographicInfo.Subtitle to be 'Subtitle1', got '%s'", formData.BibliographicInfo.Subtitle)
	}
	if formData.BibliographicInfo.SeriesTitle != "SeriesTitle1" {
		t.Errorf("Expected BibliographicInfo.SeriesTitle to be 'SeriesTitle1', got '%s'", formData.BibliographicInfo.SeriesTitle)
	}
	if formData.BibliographicInfo.Edition != "Edition1" {
		t.Errorf("Expected BibliographicInfo.Edition to be 'Edition1', got '%s'", formData.BibliographicInfo.Edition)
	}
	if formData.BibliographicInfo.TitleOfComponent != "ComponentTitle1" {
		t.Errorf("Expected BibliographicInfo.TitleOfComponent to be 'ComponentTitle1', got '%s'", formData.BibliographicInfo.TitleOfComponent)
	}
	if formData.BibliographicInfo.AuthorOfComponent != "ComponentAuthor1" {
		t.Errorf("Expected BibliographicInfo.AuthorOfComponent to be 'ComponentAuthor1', got '%s'", formData.BibliographicInfo.AuthorOfComponent)
	}
	if formData.BibliographicInfo.Volume != "Volume1" {
		t.Errorf("Expected BibliographicInfo.Volume to be 'Volume1', got '%s'", formData.BibliographicInfo.Volume)
	}
	if formData.BibliographicInfo.Issue != "Issue1" {
		t.Errorf("Expected BibliographicInfo.Issue to be 'Issue1', got '%s'", formData.BibliographicInfo.Issue)
	}
	if formData.BibliographicInfo.PagesRequested != "Pages1" {
		t.Errorf("Expected BibliographicInfo.PagesRequested to be 'Pages1', got '%s'", formData.BibliographicInfo.PagesRequested)
	}
	if formData.BibliographicInfo.EstimatedNoPages != "100" {
		t.Errorf("Expected BibliographicInfo.EstimatedNoPages to be '100', got '%s'", formData.BibliographicInfo.EstimatedNoPages)
	}
	if formData.BibliographicInfo.BibliographicItemIdentifier != "Identifier1" {
		t.Errorf("Expected BibliographicInfo.BibliographicItemIdentifier to be 'Identifier1', got '%s'", formData.BibliographicInfo.BibliographicItemIdentifier)
	}
	if formData.BibliographicInfo.BibliographicItemIdentifierCode != "Code1" {
		t.Errorf("Expected BibliographicInfo.BibliographicItemIdentifierCode to be 'Code1', got '%s'", formData.BibliographicInfo.BibliographicItemIdentifierCode)
	}
	if formData.BibliographicInfo.Sponsor != "Sponsor1" {
		t.Errorf("Expected BibliographicInfo.Sponsor to be 'Sponsor1', got '%s'", formData.BibliographicInfo.Sponsor)
	}
	if formData.BibliographicInfo.InformationSource != "Source1" {
		t.Errorf("Expected BibliographicInfo.InformationSource to be 'Source1', got '%s'", formData.BibliographicInfo.InformationSource)
	}
	if formData.BibliographicInfo.BibliographicRecordIdentifierCode != "RecordCode1" {
		t.Errorf("Expected BibliographicInfo.BibliographicRecordIdentifierCode to be 'RecordCode1', got '%s'", formData.BibliographicInfo.BibliographicRecordIdentifierCode)
	}
	if formData.BibliographicInfo.BibliographicRecordIdentifier != "RecordIdentifier1" {
		t.Errorf("Expected BibliographicInfo.BibliographicRecordIdentifier to be 'RecordIdentifier1', got '%s'", formData.BibliographicInfo.BibliographicRecordIdentifier)
	}
	if formData.PublicationInfo.Publisher != "Publisher1" {
		t.Errorf("Expected PublicationInfo.Publisher to be 'Publisher1', got '%s'", formData.PublicationInfo.Publisher)
	}
	if formData.PublicationInfo.PublicationType != "Type1" {
		t.Errorf("Expected PublicationInfo.PublicationType to be 'Type1', got '%s'", formData.PublicationInfo.PublicationType)
	}
	if formData.PublicationInfo.PublicationDate != "2025-02-24" {
		t.Errorf("Expected PublicationInfo.PublicationDate to be '2025-02-24', got '%s'", formData.PublicationInfo.PublicationDate)
	}
	if formData.PublicationInfo.PlaceOfPublication != "Place1" {
		t.Errorf("Expected PublicationInfo.PlaceOfPublication to be 'Place1', got '%s'", formData.PublicationInfo.PlaceOfPublication)
	}
	if formData.ServiceInfo.RequestType != "New" {
		t.Errorf("Expected ServiceInfo.RequestType to be 'New', got '%s'", formData.ServiceInfo.RequestType)
	}
	if formData.ServiceInfo.RequestSubType != "BookingRequest" {
		t.Errorf("Expected ServiceInfo.RequestSubType to be 'BookingRequest', got '%s'", formData.ServiceInfo.RequestSubType)
	}
	if formData.ServiceInfo.RequestingAgencyPreviousRequestId != "PreviousRequestId1" {
		t.Errorf("Expected ServiceInfo.RequestingAgencyPreviousRequestId to be 'PreviousRequestId1', got '%s'", formData.ServiceInfo.RequestingAgencyPreviousRequestId)
	}
	if formData.ServiceInfo.ServiceType != "Copy" {
		t.Errorf("Expected ServiceInfo.ServiceType to be 'Copy', got '%s'", formData.ServiceInfo.ServiceType)
	}
	if formData.ServiceInfo.ServiceLevel != "Level1" {
		t.Errorf("Expected ServiceInfo.ServiceLevel to be 'Level1', got '%s'", formData.ServiceInfo.ServiceLevel)
	}
	if formData.ServiceInfo.PreferredFormat != "Format1" {
		t.Errorf("Expected ServiceInfo.PreferredFormat to be 'Format1', got '%s'", formData.ServiceInfo.PreferredFormat)
	}
	if formData.ServiceInfo.NeedBeforeDate != "2025-03-01" {
		t.Errorf("Expected ServiceInfo.NeedBeforeDate to be '2025-03-01', got '%s'", formData.ServiceInfo.NeedBeforeDate)
	}
	if formData.ServiceInfo.CopyrightCompliance != "Compliance1" {
		t.Errorf("Expected ServiceInfo.CopyrightCompliance to be 'Compliance1', got '%s'", formData.ServiceInfo.CopyrightCompliance)
	}
	if formData.ServiceInfo.AnyEdition != "Y" {
		t.Errorf("Expected ServiceInfo.AnyEdition to be 'Y', got '%s'", formData.ServiceInfo.AnyEdition)
	}
	if formData.ServiceInfo.StartDate != "2025-02-25" {
		t.Errorf("Expected ServiceInfo.StartDate to be '2025-02-25', got '%s'", formData.ServiceInfo.StartDate)
	}
	if formData.ServiceInfo.EndDate != "2025-03-01" {
		t.Errorf("Expected ServiceInfo.EndDate to be '2025-03-01', got '%s'", formData.ServiceInfo.EndDate)
	}
	if formData.ServiceInfo.Note != "Note1" {
		t.Errorf("Expected ServiceInfo.Note to be 'Note1', got '%s'", formData.ServiceInfo.Note)
	}
	if formData.SupplierInfo.SortOrder != "1" {
		t.Errorf("Expected SupplierInfo.SortOrder to be '1', got '%s'", formData.SupplierInfo.SortOrder)
	}
	if formData.SupplierInfo.SupplierCode != "Code1" {
		t.Errorf("Expected SupplierInfo.SupplierCode to be 'Code1', got '%s'", formData.SupplierInfo.SupplierCode)
	}
	if formData.SupplierInfo.SupplierDescription != "Description1" {
		t.Errorf("Expected SupplierInfo.SupplierDescription to be 'Description1', got '%s'", formData.SupplierInfo.SupplierDescription)
	}
	if formData.SupplierInfo.CallNumber != "CallNumber1" {
		t.Errorf("Expected SupplierInfo.CallNumber to be 'CallNumber1', got '%s'", formData.SupplierInfo.CallNumber)
	}
	if formData.SupplierInfo.SummaryHoldings != "Holdings1" {
		t.Errorf("Expected SupplierInfo.SummaryHoldings to be 'Holdings1', got '%s'", formData.SupplierInfo.SummaryHoldings)
	}
	if formData.SupplierInfo.AvailabilityNote != "Note1" {
		t.Errorf("Expected SupplierInfo.AvailabilityNote to be 'Note1', got '%s'", formData.SupplierInfo.AvailabilityNote)
	}
	if formData.RequestedDeliveryInfo.SortOrder != "1" {
		t.Errorf("Expected RequestedDeliveryInfo.SortOrder to be '1', got '%s'", formData.RequestedDeliveryInfo.SortOrder)
	}
}
