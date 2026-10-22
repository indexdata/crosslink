package lms

import (
	"errors"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/ncipclient"
	"github.com/indexdata/crosslink/broker/ncipclientv1"
	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
	ncip "github.com/indexdata/crosslink/ncipv1"
)

// LmsAdapterNcip1 implements broker operations using native NCIP 1.0 types.
// Pickup routing for AcceptItem follows lib-ncip-client's ToAgency convention.
type LmsAdapterNcip1 struct {
	client *ncipclientv1.Client
	config dirapi.LmsConfig
}

var _ LmsAdapter = (*LmsAdapterNcip1)(nil)

// CreateLmsAdapterNcip1 explicitly creates an NCIP 1.0 HTTP adapter.
// LmsCreator selects this adapter when ncipVersion is 1.0.
func CreateLmsAdapterNcip1(config dirapi.LmsConfig) (LmsAdapter, error) {
	effective, err := profiles.Resolve(dirapi.Entry{LmsConfig: &config})
	if err != nil {
		return nil, err
	}
	return createResolvedLmsAdapterNcip1(*effective.LMS)
}

func createResolvedLmsAdapterNcip1(config dirapi.LmsConfig) (LmsAdapter, error) {
	if config.Address == "" || config.FromAgency == "" {
		return nil, fmt.Errorf("NCIP v1 requires address and From Agency")
	}
	return &LmsAdapterNcip1{config: config, client: ncipclientv1.New(nil, config.Address)}, nil
}

func (l *LmsAdapterNcip1) SetLogFunc(f ncipclient.NcipLogFunc) {
	l.client.SetLogFunc(ncipclientv1.LogFunc(f))
}

const ncip1Scheme = "http://www.niso.org/ncip/v1_0/"

func ncip1Agency(value string) ncip.UniqueAgencyId {
	return ncip.UniqueAgencyId{Scheme: ncip1Scheme + "schemes/agency.scm", Value: value}
}
func (l *LmsAdapterNcip1) toAgency() string {
	if l.config.ToAgency != nil {
		return *l.config.ToAgency
	}
	return "default-to-agency"
}
func (l *LmsAdapterNcip1) header() ncip.InitiationHeader {
	h := ncip.InitiationHeader{FromAgencyId: ncip.FromAgencyId{UniqueAgencyId: ncip1Agency(l.config.FromAgency)}, ToAgencyId: ncip.ToAgencyId{UniqueAgencyId: ncip1Agency(l.toAgency())}}
	if l.config.FromAgencyAuthentication != nil {
		h.FromAgencyAuthentication = *l.config.FromAgencyAuthentication
	}
	return h
}
func (l *LmsAdapterNcip1) user(id string) *ncip.UniqueUserId {
	return &ncip.UniqueUserId{UniqueAgencyId: ncip1Agency(l.toAgency()), UserIdentifierValue: id}
}
func (l *LmsAdapterNcip1) request(id string) *ncip.UniqueRequestId {
	return &ncip.UniqueRequestId{UniqueAgencyId: ncip1Agency(l.config.FromAgency), RequestIdentifierValue: id}
}
func (l *LmsAdapterNcip1) item(id string) ncip.UniqueItemId {
	return ncip.UniqueItemId{UniqueAgencyId: ncip1Agency(l.toAgency()), ItemIdentifierValue: id}
}
func ncip1ItemElements(names ...string) []ncip.ItemElementType {
	elements := make([]ncip.ItemElementType, 0, len(names))
	for _, name := range names {
		elements = append(elements, ncip.ItemElementType{Scheme: ncip1Scheme + "schemes/itemelementtype/itemelementtype.scm", Value: name})
	}
	return elements
}
func ncip1Required(values ...string) error {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("NCIP v1 requires nonempty identifiers")
		}
	}
	return nil
}

func (l *LmsAdapterNcip1) LookupUser(patron string, options LookupUserOptions) (LookupUserResult, error) {
	if l.config.LookupUserEnabled != nil && !*l.config.LookupUserEnabled {
		return LookupUserResult{UserID: patron}, nil
	}
	if err := ncip1Required(patron); err != nil {
		return LookupUserResult{}, err
	}
	names := []string{"Visible User Id"}
	if options.ValidatePatronProfile {
		names = append(names, "User Privilege")
	}
	if options.IncludePatronInfo {
		names = append(names, "Name Information", "User Address Information")
	}
	arg := ncip.LookupUser{InitiationHeader: l.header(), UniqueUserId: l.user(patron)}
	for _, name := range names {
		arg.UserElementType = append(arg.UserElementType, ncip.UserElementType{Scheme: ncip1Scheme + "schemes/userelementtype/userelementtype.scm", Value: name})
	}
	response, err := l.client.LookupUser(arg)
	if err != nil {
		// Retry a protocol rejection as a barcode lookup, as lib-ncip-client does.
		// Transport failures must not cause another request.
		var problem *ncipclientv1.Error
		if !errors.As(err, &problem) {
			return LookupUserResult{}, err
		}
		arg.UniqueUserId = nil
		arg.AuthenticationInput = []ncip.AuthenticationInput{{AuthenticationInputData: patron,
			AuthenticationDataFormatType: ncip.AuthenticationDataFormatType{Scheme: "http://www.isi.edu/in-notes/iana/assignments/media-types/media-types", Value: "Text"},
			AuthenticationInputType:      ncip.AuthenticationInputType{Scheme: ncip1Scheme + "imp1/schemes/authenticationinputtype/authenticationinputtype.scm", Value: "Barcode Id"}}}
		response, err = l.client.LookupUser(arg)
		if err != nil {
			return LookupUserResult{}, err
		}
		patron = ""
	}
	if response.UniqueUserId != nil && response.UniqueUserId.UserIdentifierValue != "" {
		patron = response.UniqueUserId.UserIdentifierValue
	}
	fields := response.UserOptionalFields
	if patron == "" && fields != nil && len(fields.VisibleUserId) > 0 {
		patron = fields.VisibleUserId[0].VisibleUserIdentifier
	}
	if patron == "" {
		return LookupUserResult{}, fmt.Errorf("missing User ID in NCIP v1 LookupUser response")
	}
	result := LookupUserResult{UserID: patron}
	var privileges []patronPrivilege
	if fields != nil {
		for _, p := range fields.UserPrivilege {
			status := ""
			if p.UserPrivilegeStatus != nil {
				status = p.UserPrivilegeStatus.UserPrivilegeStatusType.Value
			}
			privileges = append(privileges, patronPrivilege{code: p.AgencyUserPrivilegeType.Value, status: strings.TrimSpace(status), name: p.UserPrivilegeDescription})
		}
		if options.IncludePatronInfo {
			if fields.NameInformation != nil && fields.NameInformation.PersonalNameInformation != nil {
				if name := fields.NameInformation.PersonalNameInformation.StructuredPersonalUserName; name != nil {
					result.GivenName = strings.TrimSpace(name.GivenName)
					result.Surname = strings.TrimSpace(name.Surname)
				}
			}
			seen := map[string]bool{}
			for _, address := range fields.UserAddressInformation {
				if a := address.ElectronicAddress; a != nil && strings.Contains(strings.ToLower(a.ElectronicAddressType.Value), "mail") {
					email := strings.TrimSpace(a.ElectronicAddressData)
					key := strings.ToLower(email)
					if email != "" && !seen[key] {
						result.EmailAddresses = append(result.EmailAddresses, email)
						seen[key] = true
					}
				}
			}
		}
	}
	if options.ValidatePatronProfile {
		if err := validatePatronProfileCandidates(l.config, profileCandidates(privileges)); err != nil {
			return LookupUserResult{}, err
		}
	}
	return result, nil
}

func (l *LmsAdapterNcip1) AcceptItem(itemID, requestID, userID, author, title, isbn, callNumber, pickup, action string) (*AcceptedItem, error) {
	if l.config.AcceptItemEnabled != nil && !*l.config.AcceptItemEnabled {
		return nil, nil
	}
	if err := ncip1Required(itemID, requestID, userID); err != nil {
		return nil, err
	}
	if action == "" {
		action = "Hold For Pickup"
	}
	header := l.header()
	if pickup != "" {
		header.ToAgencyId.UniqueAgencyId = ncip1Agency(pickup)
	}
	item := ncip.UniqueItemId{UniqueAgencyId: ncip1Agency(l.config.FromAgency), ItemIdentifierValue: itemID}
	biblio := &ncip.BibliographicDescription{Author: author, Title: title}
	if isbn != "" {
		biblio.BibliographicItemId = &ncip.BibliographicItemId{BibliographicItemIdentifier: isbn, BibliographicItemIdentifierCode: &ncip.BibliographicItemIdentifierCode{Scheme: ncip1Scheme + "imp1/schemes/bibliographicitemidentifiercode/bibliographicitemidentifiercode.scm", Value: "ISBN"}}
	}
	fields := &ncip.ItemOptionalFields{BibliographicDescription: biblio}
	if callNumber != "" {
		fields.ItemDescription = &ncip.ItemDescription{CallNumber: callNumber}
	}
	response, err := l.client.AcceptItem(ncip.AcceptItem{InitiationHeader: header, UniqueRequestId: *l.request(requestID), UniqueUserId: l.user(userID), UniqueItemId: &item,
		RequestedActionType:         ncip.RequestedActionType{Scheme: ncip1Scheme + "imp1/schemes/requestedactiontype/requestedactiontype.scm", Value: action},
		IndeterminateLoanPeriodFlag: &ncip.IndeterminateLoanPeriodFlag{}, ItemOptionalFields: fields})
	if err != nil {
		return nil, err
	}
	if response.UniqueItemId != nil {
		item = *response.UniqueItemId
	}
	return &AcceptedItem{ItemID: encodeNcip1ItemID(item)}, nil
}

func (l *LmsAdapterNcip1) DeleteItem(string) (bool, error) {
	if l.config.AcceptItemEnabled != nil && !*l.config.AcceptItemEnabled {
		return false, nil
	}
	return false, fmt.Errorf("NCIP 1.0: %w", ErrDeleteItemUnsupported)
}

func (l *LmsAdapterNcip1) RequestItem(requestID, itemID, userID, _, itemLocation string) (*RequestedItem, error) {
	if l.config.RequestItemEnabled != nil && !*l.config.RequestItemEnabled {
		return nil, nil
	}
	if err := ncip1Required(requestID, itemID, userID); err != nil {
		return nil, err
	}
	if itemLocation != "" {
		return nil, fmt.Errorf("NCIP v1 RequestItem does not support item location constraints")
	}
	code := "SYSNUMBER"
	if l.config.RequestItemBibIdCode != nil {
		code = *l.config.RequestItemBibIdCode
	}
	if l.config.BibIdNormalization != nil && *l.config.BibIdNormalization == "sierra" {
		itemID = strings.TrimPrefix(itemID, ".b")
		if len(itemID) > 1 && itemID[len(itemID)-1] >= '0' && itemID[len(itemID)-1] <= '9' {
			itemID = itemID[:len(itemID)-1]
		}
	}
	response, err := l.client.RequestItem(ncip.RequestItem{
		InitiationHeader: l.header(),
		UniqueUserId:     l.user(userID),
		UniqueRequestId:  l.request(requestID),
		UniqueBibliographicId: &ncip.UniqueBibliographicId{
			BibliographicRecordId: &ncip.BibliographicRecordId{
				BibliographicRecordIdentifier: itemID,
				BibliographicRecordIdentifierCode: &ncip.BibliographicRecordIdentifierCode{
					Scheme: ncip1Scheme + "imp1/schemes/bibliographicrecordidentifiercode/bibliographicrecordidentifiercode.scm",
					Value:  code,
				},
			},
		},
		RequestType:      l.requestType(),
		RequestScopeType: l.requestScope(),
		ItemElementType:  ncip1ItemElements("Bibliographic Description", "Item Description", "Location"),
	})
	if err != nil {
		return nil, err
	}
	result := &RequestedItem{RequestID: encodeNcip1RequestID(*l.request(requestID))}
	if response.UniqueItemId != nil {
		result.ItemID = encodeNcip1ItemID(*response.UniqueItemId)
	}
	if response.UniqueRequestId != nil && strings.TrimSpace(response.UniqueRequestId.RequestIdentifierValue) != "" {
		result.RequestID = encodeNcip1RequestID(*response.UniqueRequestId)
	}
	if f := response.ItemOptionalFields; f != nil {
		if f.BibliographicDescription != nil {
			result.Title = f.BibliographicDescription.Title
		}
		if d := f.ItemDescription; d != nil {
			result.CallNumber = d.CallNumber
			if d.VisibleItemId != nil && strings.EqualFold(d.VisibleItemId.VisibleItemIdentifierType.Value, "Item Barcode") {
				result.Barcode = d.VisibleItemId.VisibleItemIdentifier
			}
			if result.Barcode == "" {
				result.Barcode = d.CopyNumber
			}
		}
		result.Location, result.ShelvingLocation = ncip1ItemLocations(f)
	}
	return result, nil
}

func (l *LmsAdapterNcip1) requestType() ncip.RequestType {
	value := "Page"
	if l.config.RequestItemRequestType != nil {
		value = *l.config.RequestItemRequestType
	}
	return ncip.RequestType{Scheme: ncip1Scheme + "imp1/schemes/requesttype/requesttype.scm", Value: value}
}
func (l *LmsAdapterNcip1) requestScope() ncip.RequestScopeType {
	value := "Item"
	if l.config.RequestItemRequestScopeType != nil {
		value = *l.config.RequestItemRequestScopeType
	}
	return ncip.RequestScopeType{Scheme: ncip1Scheme + "imp1/schemes/requestscopetype/requestscopetype.scm", Value: value}
}
func (l *LmsAdapterNcip1) CancelRequestItem(requestID, userID string) error {
	if err := ncip1Required(requestID, userID); err != nil {
		return err
	}
	request, err := l.savedRequest(requestID)
	if err != nil {
		return err
	}
	scope := l.requestScope()
	_, err = l.client.CancelRequestItem(ncip.CancelRequestItem{InitiationHeader: l.header(), UniqueUserId: l.user(userID), UniqueRequestId: request, RequestType: l.requestType(), RequestScopeType: &scope})
	return err
}
func (l *LmsAdapterNcip1) CheckInItem(itemID string) (bool, error) {
	if l.config.CheckInItemEnabled != nil && !*l.config.CheckInItemEnabled {
		return false, nil
	}
	if err := ncip1Required(itemID); err != nil {
		return false, err
	}
	item, err := l.savedItem(itemID)
	if err != nil {
		return false, err
	}
	_, err = l.client.CheckInItem(ncip.CheckInItem{InitiationHeader: l.header(), UniqueItemId: item})
	return err == nil, err
}
func (l *LmsAdapterNcip1) CheckOutItem(requestID, itemID, userID, externalReference string) (*CheckedOutItem, error) {
	if l.config.CheckOutItemEnabled != nil && !*l.config.CheckOutItemEnabled {
		return nil, nil
	}
	if err := ncip1Required(itemID, userID); err != nil {
		return nil, err
	}
	if externalReference != "" {
		return nil, fmt.Errorf("NCIP v1 CheckOutItem does not support an external reference")
	}
	item, err := l.savedItem(itemID)
	if err != nil {
		return nil, err
	}
	arg := ncip.CheckOutItem{InitiationHeader: l.header(), UniqueItemId: item, UniqueUserId: l.user(userID), ItemElementType: ncip1ItemElements("Bibliographic Description")}
	if requestID != "" {
		request, err := l.savedRequest(requestID)
		if err != nil {
			return nil, err
		}
		arg.UniqueRequestId = request
	}
	response, err := l.client.CheckOutItem(arg)
	if err != nil {
		return nil, err
	}
	result := &CheckedOutItem{}
	if response.ItemOptionalFields != nil && response.ItemOptionalFields.BibliographicDescription != nil {
		result.Title = response.ItemOptionalFields.BibliographicDescription.Title
	}
	if response.DateDue != nil && !response.DateDue.IsZero() && response.DateDue.Year() > 0 {
		due := response.DateDue.Time
		result.DueDate = &due
	}
	return result, nil
}
func (l *LmsAdapterNcip1) CreateUserFiscalTransaction(string, string) error {
	return fmt.Errorf("NCIP v1 fiscal transactions require transaction type, action and amount, which the LMS interface does not supply")
}

// RequestItemUsesPickupLocation is false: v1 has no direct pickup location field.
func (l *LmsAdapterNcip1) RequestItemUsesPickupLocation() bool { return false }

func ncip1ItemLocations(fields *ncip.ItemOptionalFields) (string, string) {
	if fields == nil {
		return "", ""
	}
	for _, itemLocation := range fields.Location {
		names := itemLocation.LocationName.LocationNameInstance
		var location, shelvingLocation string
		for _, name := range names {
			if strings.TrimSpace(name.LocationNameValue) == "" {
				continue
			}
			switch name.LocationNameLevel {
			case 1:
				if location == "" {
					location = strings.TrimSpace(name.LocationNameValue)
				}
			case 2:
				if shelvingLocation == "" {
					shelvingLocation = strings.TrimSpace(name.LocationNameValue)
				}
			}
		}
		if shelvingLocation == "" && location != "" && len(names) == 1 {
			shelvingLocation = location
		}
		if location != "" || shelvingLocation != "" {
			return location, shelvingLocation
		}
	}
	return "", ""
}

func (l *LmsAdapterNcip1) InstitutionalPatron(requesterSymbol string) string {
	patron := "INST-{requesterSymbol}"
	if l.config.RequesterPatronPattern != nil {
		patron = *l.config.RequesterPatronPattern
	}
	return strings.ReplaceAll(patron, "{requesterSymbol}", strings.ToUpper(requesterSymbol))
}

func (l *LmsAdapterNcip1) SupplierPickupLocation() string {
	if l.config.SupplierPickupLocation != nil {
		return *l.config.SupplierPickupLocation
	}
	return "ILL Office"
}

func (l *LmsAdapterNcip1) ItemLocation() string {
	if l.config.ItemLocation != nil {
		return *l.config.ItemLocation
	}
	return ""
}

func (l *LmsAdapterNcip1) RequesterPickupLocation() string {
	if l.config.RequesterPickupLocation != nil {
		return *l.config.RequesterPickupLocation
	}
	return "Main Library"
}

func (l *LmsAdapterNcip1) AcceptItemUsesPickupLocation() bool {
	return l.config.AcceptItemEnabled == nil || *l.config.AcceptItemEnabled
}
