package lms

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/indexdata/crosslink/broker/ncipclient"
	"github.com/indexdata/crosslink/ncip"
)

type NcipProperty string

const (
	FromAgency               NcipProperty = "from_agency"
	FromAgencyAuthentication NcipProperty = "from_agency_authentication"
	ToAgency                 NcipProperty = "to_agency"
	Address                  NcipProperty = "address"
)

// NCIP LMS Adapter, based on:
// https://github.com/openlibraryenvironment/mod-rs/blob/master/service/grails-app/services/org/olf/rs/hostlms/BaseHostLMSService.groovy
// https://github.com/openlibraryenvironment/lib-ncip-client/tree/master/lib-ncip-client/src/main/java/org/olf/rs/circ/client

type LmsAdapterNcip struct {
	ncipClient               ncipclient.NcipClient
	address                  string
	toAgency                 string
	fromAgency               string
	fromAgencyAuthentication string
}

func setField(m map[string]any, field NcipProperty, dst *string) error {
	v, ok := m[string(field)].(string)
	if !ok || v == "" {
		return fmt.Errorf("missing required NCIP configuration field: %s", field)
	}
	*dst = v
	return nil
}

func optField(m map[string]any, field NcipProperty, dst *string, def string) {
	v, ok := m[string(field)].(string)
	if !ok || v == "" {
		*dst = def
	} else {
		*dst = v
	}
}

func (l *LmsAdapterNcip) parseConfig(ncipInfo map[string]any) error {
	err := setField(ncipInfo, Address, &l.address)
	if err != nil {
		return err
	}
	err = setField(ncipInfo, FromAgency, &l.fromAgency)
	if err != nil {
		return err
	}
	optField(ncipInfo, FromAgencyAuthentication, &l.fromAgencyAuthentication, "")
	optField(ncipInfo, ToAgency, &l.toAgency, "default-to-agency")
	return nil
}

func CreateLmsAdapterNcip(ncipInfo map[string]any) (LmsAdapter, error) {
	l := &LmsAdapterNcip{}
	err := l.parseConfig(ncipInfo)
	if err != nil {
		return nil, err
	}
	l.ncipClient = ncipclient.NewNcipClient(http.DefaultClient, l.address, l.fromAgency, l.toAgency, l.fromAgencyAuthentication)
	return l, nil
}

func (l *LmsAdapterNcip) LookupUser(patron string) (string, error) {
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
	// then try by user user name
	// a better solution would be that the LookupUser had type argument (eg barcode or PIN)
	// but this is now mod-rs does it
	var authenticationInput []ncip.AuthenticationInput
	authenticationInput = append(authenticationInput, ncip.AuthenticationInput{
		AuthenticationInputType: ncip.SchemeValuePair{Text: "username"},
		AuthenticationInputData: patron,
	})
	userElements := []ncip.SchemeValuePair{{Text: "User Id"}}
	arg = ncip.LookupUser{
		AuthenticationInput: authenticationInput,
		UserElementType:     userElements,
	}
	response, err := l.ncipClient.LookupUser(arg)
	if err != nil {
		return "", err
	}
	if response.UserId == nil {
		return "", fmt.Errorf("missing User ID in LookupUser response")
	}
	return response.UserId.UserIdentifierValue, nil
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

func (l *LmsAdapterNcip) DeleteItem(itemId string) (string, error) {
	arg := ncip.DeleteItem{
		ItemId: ncip.ItemId{ItemIdentifierValue: itemId},
	}
	res, err := l.ncipClient.DeleteItem(arg)
	if err == nil && res != nil && res.ItemId != nil {
		return res.ItemId.ItemIdentifierValue, nil
	}
	return itemId, err
}

func (l *LmsAdapterNcip) RequestItem(
	requestId string,
	itemId string,
	borrowerBarcode string,
	pickupLocation string,
	itemLocation string,
) error {
	// mod-rs: see getRequestItemPickupLocation which in some cases overrides pickupLocation
	var pickupLocationField *ncip.SchemeValuePair
	if pickupLocation != "" {
		pickupLocationField = &ncip.SchemeValuePair{Text: pickupLocation}
	}
	var userIdField *ncip.UserId
	if borrowerBarcode != "" {
		userIdField = &ncip.UserId{UserIdentifierValue: borrowerBarcode}
	}
	bibIdField := ncip.BibliographicId{
		BibliographicRecordId: &ncip.BibliographicRecordId{
			BibliographicRecordIdentifier:     itemId,
			BibliographicRecordIdentifierCode: &ncip.SchemeValuePair{Text: "SYSNUMBER"},
		}}
	requestScopeTypeField := ncip.SchemeValuePair{Text: "Item"} // or "Title"

	// mod-rs: getRequestItemRequestType()
	requestTypeField := ncip.SchemeValuePair{Text: "Page"} // "Loan" in Base
	arg := ncip.RequestItem{
		RequestId:        &ncip.RequestId{RequestIdentifierValue: requestId},
		BibliographicId:  []ncip.BibliographicId{bibIdField},
		UserId:           userIdField,
		PickupLocation:   pickupLocationField,
		RequestType:      requestTypeField,
		RequestScopeType: requestScopeTypeField,
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
	itemElements := []ncip.SchemeValuePair{
		{Text: "Bibliographic Description"},
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
	borrowerBarcode string,
	externalReferenceValue string,
) error {
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
		UserId:    &ncip.UserId{UserIdentifierValue: borrowerBarcode},
		ItemId:    ncip.ItemId{ItemIdentifierValue: itemBarcode},
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
