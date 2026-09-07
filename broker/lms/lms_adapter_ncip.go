package lms

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/broker/ncipclient"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/ncip"
)

type NcipUserElement string

const (
	NCIPUserId        string = "User Id"
	NCIPUserPrivilege string = "User Privilege"
	NCIPItemBarcode   string = "Item Barcode"
)

type NcipItemElement string

const (
	NCIPBibliographicDescription NcipItemElement = "Bibliographic Description"
)

// NCIP LMS Adapter, based on:
// https://github.com/openlibraryenvironment/mod-rs/blob/master/service/grails-app/services/org/olf/rs/hostlms/BaseHostLMSService.groovy
// https://github.com/openlibraryenvironment/lib-ncip-client/tree/master/lib-ncip-client/src/main/java/org/olf/rs/circ/client

type LmsAdapterNcip struct {
	ncipClient ncipclient.NcipClient
	config     dirapi.LmsConfig
}

func (l *LmsAdapterNcip) requestItemRequestType() string {
	if l.config.RequestItemRequestType != nil {
		return *l.config.RequestItemRequestType
	}
	return "Page"
}

func (l *LmsAdapterNcip) requestItemRequestScopeType() string {
	if l.config.RequestItemRequestScopeType != nil {
		return *l.config.RequestItemRequestScopeType
	}
	return "Item"
}

func CreateLmsAdapterNcip(lmsConfig dirapi.LmsConfig) (LmsAdapter, error) {
	l := &LmsAdapterNcip{config: lmsConfig}
	toAgency := "default-to-agency"
	if l.config.ToAgency != nil {
		toAgency = *l.config.ToAgency
	}
	FromAgencyAuthentication := ""
	if l.config.FromAgencyAuthentication != nil {
		FromAgencyAuthentication = *l.config.FromAgencyAuthentication
	}
	if l.config.Address == "" {
		return nil, fmt.Errorf("missing NCIP address in LMS configuration")
	}
	if l.config.FromAgency == "" {
		return nil, fmt.Errorf("missing From Agency in LMS configuration")
	}
	l.ncipClient = ncipclient.NewNcipClient(http.DefaultClient, l.config.Address, l.config.FromAgency, toAgency, FromAgencyAuthentication)
	return l, nil
}

func (l *LmsAdapterNcip) SetLogFunc(logFunc ncipclient.NcipLogFunc) {
	l.ncipClient.SetLogFunc(logFunc)
}

func (l *LmsAdapterNcip) LookupUser(patron string, validatePatronProfile bool) (string, error) {
	if l.config.LookupUserEnabled != nil && !*l.config.LookupUserEnabled {
		return patron, nil // could even be empty
	}
	if patron == "" {
		return "", fmt.Errorf("empty patron identifier")
	}
	// first try to check if patron is actually user Id
	arg := ncip.LookupUser{
		UserId:          &ncip.UserId{UserIdentifierValue: patron},
		UserElementType: l.getUserElements(false, validatePatronProfile),
	}
	response, err := l.ncipClient.LookupUser(arg)
	if err == nil {
		if validatePatronProfile {
			if err = l.validatePatronProfile(response); err != nil {
				return "", err
			}
		}
		return patron, nil
	}
	// then try by user username
	// a better solution would be that the LookupUser had type argument (eg barcode or PIN)
	// but this is how mod-rs does it
	var authenticationInput []ncip.AuthenticationInput
	authenticationInput = append(authenticationInput, ncip.AuthenticationInput{
		AuthenticationInputType: ncip.SchemeValuePair{Text: "username"},
		AuthenticationInputData: patron,
	})
	arg = ncip.LookupUser{
		AuthenticationInput: authenticationInput,
		UserElementType:     l.getUserElements(true, validatePatronProfile),
	}
	response, err = l.ncipClient.LookupUser(arg)
	if err != nil {
		return "", err
	}
	if validatePatronProfile {
		if err = l.validatePatronProfile(response); err != nil {
			return "", err
		}
	}
	if response != nil && response.UserOptionalFields != nil && len(response.UserOptionalFields.UserId) != 0 {
		return response.UserOptionalFields.UserId[0].UserIdentifierValue, nil
	}
	if response != nil && response.UserId != nil {
		return response.UserId.UserIdentifierValue, nil
	}
	return "", fmt.Errorf("missing User ID in LookupUser response")
}

func (l *LmsAdapterNcip) getUserElements(userId bool, validatePatronProfile bool) []ncip.SchemeValuePair {
	if validatePatronProfile && l.config.PatronProfiles != nil && len(*l.config.PatronProfiles) > 0 {
		return []ncip.SchemeValuePair{
			{Text: NCIPUserId},
			{Text: NCIPUserPrivilege},
		}
	}
	if userId {
		return []ncip.SchemeValuePair{
			{Text: NCIPUserId},
		}
	}
	return nil
}

func (l *LmsAdapterNcip) validatePatronProfile(response *ncip.LookupUserResponse) error {
	if l.config.PatronProfiles == nil {
		return nil
	}
	candidates := patronProfileCandidates(response)
	if len(candidates) == 0 {
		// Keep an empty candidate so a rule with neither code nor name can
		// provide the configured default when no profile was returned.
		candidates = []patronProfileCandidate{{}}
	}
	for _, profile := range *l.config.PatronProfiles {
		for _, candidate := range candidates {
			codeMatches := profile.Code == nil || strings.EqualFold(strings.TrimSpace(*profile.Code), candidate.code)
			nameMatches := profile.Name == nil || strings.EqualFold(strings.TrimSpace(*profile.Name), candidate.name)
			if codeMatches && nameMatches {
				if !profile.CanCreateRequests {
					return &PatronProfileIneligibleError{ProfileCode: candidate.code, ProfileName: candidate.name}
				}
				return nil
			}
		}
	}
	return nil
}

type patronProfileCandidate struct {
	code string
	name string
}

func patronProfileCandidates(response *ncip.LookupUserResponse) []patronProfileCandidate {
	if response == nil || response.UserOptionalFields == nil {
		return nil
	}
	privileges := response.UserOptionalFields.UserPrivilege

	// Most implementations return PROFILE as the privilege type and the
	// patron profile code as its status value. Treat this explicit discriminator
	// as authoritative when it is present.
	var candidates []patronProfileCandidate
	for _, privilege := range privileges {
		privilegeType := strings.TrimSpace(privilege.AgencyUserPrivilegeType.Text)
		status := userPrivilegeStatus(privilege)
		if strings.EqualFold(privilegeType, "PROFILE") && status != "" {
			candidates = append(candidates, patronProfileCandidate{
				code: status,
				name: strings.TrimSpace(privilege.UserPrivilegeDescription),
			})
		}
	}
	if len(candidates) > 0 {
		return candidates
	}

	// Other implementations put the profile code in the privilege type and
	// use Active or OK as its status. All such privileges are candidates: NCIP
	// permits UserPrivilege to repeat and unrelated privileges may come first.
	for _, privilege := range privileges {
		privilegeType := strings.TrimSpace(privilege.AgencyUserPrivilegeType.Text)
		status := userPrivilegeStatus(privilege)
		if privilegeType != "" && (strings.EqualFold(status, "ACTIVE") || strings.EqualFold(status, "OK")) {
			candidates = append(candidates, patronProfileCandidate{
				code: privilegeType,
				name: strings.TrimSpace(privilege.UserPrivilegeDescription),
			})
		}
	}
	if len(candidates) > 0 {
		return candidates
	}

	// Some implementations return just one privilege containing the profile
	// code, without a status.
	if len(privileges) == 1 && userPrivilegeStatus(privileges[0]) == "" {
		return []patronProfileCandidate{{
			code: strings.TrimSpace(privileges[0].AgencyUserPrivilegeType.Text),
			name: strings.TrimSpace(privileges[0].UserPrivilegeDescription),
		}}
	}
	return nil
}

func userPrivilegeStatus(privilege ncip.UserPrivilege) string {
	if privilege.UserPrivilegeStatus == nil {
		return ""
	}
	return strings.TrimSpace(privilege.UserPrivilegeStatus.UserPrivilegeStatusType.Text)
}

func (l *LmsAdapterNcip) AcceptItem(
	itemId string,
	requestId string,
	userId string,
	author string,
	title string,
	isbn string,
	callNumber string,
	pickupLocation string,
	requestedAction string,
) error {
	if l.config.AcceptItemEnabled != nil && !*l.config.AcceptItemEnabled {
		return nil
	}
	var bibliographicItemId *ncip.BibliographicItemId
	if isbn != "" {
		bibliographicItemId = &ncip.BibliographicItemId{
			BibliographicItemIdentifier:     isbn,
			BibliographicItemIdentifierCode: &ncip.SchemeValuePair{Text: "ISBN"},
		}
	}
	biblioInfo := &ncip.BibliographicDescription{
		Author:              author,
		Title:               title,
		BibliographicItemId: bibliographicItemId,
	}
	var itemDescription *ncip.ItemDescription
	if callNumber != "" {
		itemDescription = &ncip.ItemDescription{CallNumber: callNumber}
	}
	itemOptionalFields := &ncip.ItemOptionalFields{
		BibliographicDescription: biblioInfo,
		ItemDescription:          itemDescription,
	}
	var pickupLocationField *ncip.SchemeValuePair
	if pickupLocation != "" {
		pickupLocationField = &ncip.SchemeValuePair{Text: pickupLocation}
	}
	if requestedAction == "" {
		requestedAction = "Hold For Pickup"
	}
	arg := ncip.AcceptItem{
		RequestId:           ncip.RequestId{RequestIdentifierValue: requestId},
		RequestedActionType: ncip.SchemeValuePair{Text: requestedAction},
		UserId:              &ncip.UserId{UserIdentifierValue: userId},
		ItemId:              &ncip.ItemId{ItemIdentifierValue: itemId},
		ItemOptionalFields:  itemOptionalFields,
		PickupLocation:      pickupLocationField,
	}
	_, err := l.ncipClient.AcceptItem(arg)
	return err
}

func (l *LmsAdapterNcip) DeleteItem(itemId string) error {
	arg := ncip.DeleteItem{
		ItemId: ncip.ItemId{ItemIdentifierValue: itemId},
	}
	_, err := l.ncipClient.DeleteItem(arg)
	return err
}

func (l *LmsAdapterNcip) RequestItem(
	requestId string,
	itemId string,
	userId string,
	pickupLocation string,
	itemLocation string,
) (*RequestedItem, error) {
	if l.config.RequestItemEnabled != nil && !*l.config.RequestItemEnabled {
		return nil, nil
	}
	if strings.TrimSpace(requestId) == "" {
		return nil, fmt.Errorf("missing request ID for RequestItem")
	}
	var pickupLocationField *ncip.SchemeValuePair
	if pickupLocation != "" && (l.config.RequestItemPickupLocationEnabled == nil || *l.config.RequestItemPickupLocationEnabled) {
		pickupLocationField = &ncip.SchemeValuePair{Text: pickupLocation}
	}
	var userIdField *ncip.UserId
	if userId != "" {
		userIdField = &ncip.UserId{UserIdentifierValue: userId}
	}
	code := "SYSNUMBER"
	if l.config.RequestItemBibIdCode != nil {
		code = *l.config.RequestItemBibIdCode
	}
	bibIdField := ncip.BibliographicId{
		BibliographicRecordId: &ncip.BibliographicRecordId{
			BibliographicRecordIdentifier:     itemId,
			BibliographicRecordIdentifierCode: &ncip.SchemeValuePair{Text: code},
		}}
	requestScopeTypeField := ncip.SchemeValuePair{Text: l.requestItemRequestScopeType()}
	requestTypeField := ncip.SchemeValuePair{Text: l.requestItemRequestType()}

	var itemOptionalFields *ncip.ItemOptionalFields
	if itemLocation != "" {
		locationNameInstance := ncip.LocationNameInstance{
			LocationNameLevel: 1,
			LocationNameValue: itemLocation,
		}
		locationName := ncip.LocationName{
			LocationNameInstance: []ncip.LocationNameInstance{locationNameInstance},
		}
		location := ncip.Location{
			LocationName: locationName,
		}
		itemOptionalFields = &ncip.ItemOptionalFields{
			Location: []ncip.Location{location},
		}
	}
	itemElements := []ncip.SchemeValuePair{
		{Text: string(NCIPBibliographicDescription)},
	}
	arg := ncip.RequestItem{
		RequestId:          &ncip.RequestId{RequestIdentifierValue: requestId},
		BibliographicId:    []ncip.BibliographicId{bibIdField},
		UserId:             userIdField,
		PickupLocation:     pickupLocationField,
		RequestType:        requestTypeField,
		RequestScopeType:   requestScopeTypeField,
		ItemOptionalFields: itemOptionalFields,
		ItemElementType:    itemElements,
	}
	response, err := l.ncipClient.RequestItem(arg)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("empty response from RequestItem")
	}
	barcode := ""
	callNumber := ""
	if response.ItemId != nil && response.ItemId.ItemIdentifierType != nil &&
		response.ItemId.ItemIdentifierType.Text == NCIPItemBarcode {
		barcode = response.ItemId.ItemIdentifierValue
	}
	if response.ItemOptionalFields != nil && response.ItemOptionalFields.ItemDescription != nil {
		callNumber = response.ItemOptionalFields.ItemDescription.CallNumber
		if barcode == "" {
			barcode = response.ItemOptionalFields.ItemDescription.CopyNumber
		}
	}
	title := ""
	if response.ItemOptionalFields != nil && response.ItemOptionalFields.BibliographicDescription != nil {
		title = response.ItemOptionalFields.BibliographicDescription.Title
	}
	lmsRequestID := requestId
	if response.RequestId != nil && strings.TrimSpace(response.RequestId.RequestIdentifierValue) != "" {
		lmsRequestID = response.RequestId.RequestIdentifierValue
	}
	return &RequestedItem{
		RequestID:  lmsRequestID,
		Barcode:    barcode,
		CallNumber: callNumber,
		Title:      title,
	}, nil
}

func (l *LmsAdapterNcip) CancelRequestItem(requestId string, userId string) error {
	arg := ncip.CancelRequestItem{
		UserId:           &ncip.UserId{UserIdentifierValue: userId},
		RequestId:        &ncip.RequestId{RequestIdentifierValue: requestId},
		RequestType:      ncip.SchemeValuePair{Text: l.requestItemRequestType()},
		RequestScopeType: &ncip.SchemeValuePair{Text: l.requestItemRequestScopeType()},
	}
	_, err := l.ncipClient.CancelRequestItem(arg)
	return err
}

func (l *LmsAdapterNcip) CheckInItem(itemId string) error {
	if l.config.CheckInItemEnabled != nil && !*l.config.CheckInItemEnabled {
		return nil
	}
	itemElements := []ncip.SchemeValuePair{
		{Text: string(NCIPBibliographicDescription)},
	}
	arg := ncip.CheckInItem{
		ItemId:          ncip.ItemId{ItemIdentifierValue: itemId},
		ItemElementType: itemElements,
	}
	_, err := l.ncipClient.CheckInItem(arg)
	// mod-rs does not seem to use the Bibliographic Description in response
	return err
}

func (l *LmsAdapterNcip) CheckOutItem(
	requestId string,
	itemBarcode string,
	userId string,
	externalReferenceValue string,
) (string, error) {
	if l.config.CheckOutItemEnabled != nil && !*l.config.CheckOutItemEnabled {
		return "", nil
	}
	var ext *ncip.Ext
	if externalReferenceValue != "" {
		externalId := ncip.RequestId{RequestIdentifierValue: externalReferenceValue}
		bytes, err := xml.Marshal(externalId)
		if err != nil {
			return "", err
		}
		ext = &ncip.Ext{XMLContent: bytes}
	}
	itemElements := []ncip.SchemeValuePair{
		{Text: string(NCIPBibliographicDescription)},
	}
	var requestID *ncip.RequestId
	if requestId != "" {
		requestID = &ncip.RequestId{RequestIdentifierValue: requestId}
	}
	arg := ncip.CheckOutItem{
		RequestId:       requestID,
		UserId:          &ncip.UserId{UserIdentifierValue: userId},
		ItemId:          ncip.ItemId{ItemIdentifierValue: itemBarcode},
		ItemElementType: itemElements,
		Ext:             ext,
	}
	response, err := l.ncipClient.CheckOutItem(arg)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", fmt.Errorf("empty response from CheckOutItem")
	}
	title := ""
	if response.ItemOptionalFields != nil && response.ItemOptionalFields.BibliographicDescription != nil {
		title = response.ItemOptionalFields.BibliographicDescription.Title
	}
	return title, nil
}

func (l *LmsAdapterNcip) CreateUserFiscalTransaction(userId string, itemId string) error {
	arg := ncip.CreateUserFiscalTransaction{
		UserId: &ncip.UserId{UserIdentifierValue: userId},
	}
	_, err := l.ncipClient.CreateUserFiscalTransaction(arg)
	return err
}

func (l *LmsAdapterNcip) InstitutionalPatron(requesterSymbol string) string {
	patron := "INST-{requesterSymbol}"
	if l.config.RequesterPatronPattern != nil {
		patron = *l.config.RequesterPatronPattern
	}
	return strings.ReplaceAll(patron, "{requesterSymbol}", strings.ToUpper(requesterSymbol))
}

func (l *LmsAdapterNcip) SupplierPickupLocation() string {
	if l.config.SupplierPickupLocation != nil {
		return *l.config.SupplierPickupLocation
	}
	return "ILL Office"
}

func (l *LmsAdapterNcip) ItemLocation() string {
	if l.config.ItemLocation != nil {
		return *l.config.ItemLocation
	}
	return ""
}

func (l *LmsAdapterNcip) RequesterPickupLocation() string {
	if l.config.RequesterPickupLocation != nil {
		return *l.config.RequesterPickupLocation
	}
	return "Main Library"
}
