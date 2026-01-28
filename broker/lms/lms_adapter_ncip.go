package lms

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/broker/ncipclient"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/ncip"
)

type NcipUserElement string

const (
	NCIPUserId NcipUserElement = "User Id"
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
	config     directory.LmsConfig
}

func CreateLmsAdapterNcip(lmsConfig directory.LmsConfig) (LmsAdapter, error) {
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

func (l *LmsAdapterNcip) LookupUser(patron string) (string, error) {
	if l.config.LookupUserEnabled != nil && !*l.config.LookupUserEnabled {
		return patron, nil // could even be empty
	}
	if patron == "" {
		return "", fmt.Errorf("empty patron identifier")
	}
	// first try to check if patron is actually user Id
	arg := ncip.LookupUser{
		UserId: &ncip.UserId{UserIdentifierValue: patron},
	}
	_, err := l.ncipClient.LookupUser(arg)
	if err == nil {
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
	userElements := []ncip.SchemeValuePair{{Text: string(NCIPUserId)}}
	arg = ncip.LookupUser{
		AuthenticationInput: authenticationInput,
		UserElementType:     userElements,
	}
	response, err := l.ncipClient.LookupUser(arg)
	if err != nil {
		return "", err
	}
	if response.UserOptionalFields != nil && len(response.UserOptionalFields.UserId) != 0 {
		return response.UserOptionalFields.UserId[0].UserIdentifierValue, nil
	}
	if response.UserId != nil {
		return response.UserId.UserIdentifierValue, nil
	}
	return "", fmt.Errorf("missing User ID in LookupUser response")
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
) error {
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
	scopeType := "Item"
	if l.config.RequestItemRequestScopeType != nil {
		scopeType = *l.config.RequestItemRequestScopeType
	}
	requestScopeTypeField := ncip.SchemeValuePair{Text: scopeType}

	requestType := "Page"
	if l.config.RequestItemRequestType != nil {
		requestType = *l.config.RequestItemRequestType
	}
	requestTypeField := ncip.SchemeValuePair{Text: requestType}

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
	arg := ncip.RequestItem{
		RequestId:          &ncip.RequestId{RequestIdentifierValue: requestId},
		BibliographicId:    []ncip.BibliographicId{bibIdField},
		UserId:             userIdField,
		PickupLocation:     pickupLocationField,
		RequestType:        requestTypeField,
		RequestScopeType:   requestScopeTypeField,
		ItemOptionalFields: itemOptionalFields,
	}
	_, err := l.ncipClient.RequestItem(arg)
	return err
}

func (l *LmsAdapterNcip) CancelRequestItem(requestId string, userId string) error {
	arg := ncip.CancelRequestItem{
		UserId:    &ncip.UserId{UserIdentifierValue: userId},
		RequestId: &ncip.RequestId{RequestIdentifierValue: requestId},
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
	itemId string,
	userId string,
	externalReferenceValue string,
) error {
	if l.config.CheckOutItemEnabled != nil && !*l.config.CheckOutItemEnabled {
		return nil
	}
	var ext *ncip.Ext
	if externalReferenceValue != "" {
		externalId := ncip.RequestId{RequestIdentifierValue: externalReferenceValue}
		bytes, err := xml.Marshal(externalId)
		if err != nil {
			return err
		}
		ext = &ncip.Ext{XMLContent: bytes}
	}
	arg := ncip.CheckOutItem{
		RequestId: &ncip.RequestId{RequestIdentifierValue: requestId},
		UserId:    &ncip.UserId{UserIdentifierValue: userId},
		ItemId:    ncip.ItemId{ItemIdentifierValue: itemId},
		Ext:       ext,
	}
	_, err := l.ncipClient.CheckOutItem(arg)
	return err
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
