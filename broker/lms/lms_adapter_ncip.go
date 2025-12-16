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
	FromAgency                      NcipProperty = "from_agency"
	FromAgencyAuthentication        NcipProperty = "from_agency_authentication"
	ToAgency                        NcipProperty = "to_agency"
	Address                         NcipProperty = "address"
	LookupUserEnable                NcipProperty = "lookup_user_enable"
	AcceptItemEnable                NcipProperty = "accept_item_enable"
	CheckInItemEnable               NcipProperty = "check_in_item_enable"
	CheckOutItemEnable              NcipProperty = "check_out_item_enable"
	RequestItemRequestType          NcipProperty = "request_item_request_type"
	RequestItemRequestScopeType     NcipProperty = "request_item_request_scope_type"
	RequestItemBibIdCode            NcipProperty = "request_item_bib_id_code"
	RequestItemPickupLocationEnable NcipProperty = "request_item_pickup_location_enable"
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
	ncipClient                      ncipclient.NcipClient
	address                         string
	toAgency                        string
	fromAgency                      string
	fromAgencyAuthentication        string
	lookupUserEnable                bool
	acceptItemEnable                bool
	checkInItemEnable               bool
	checkOutItemEnable              bool
	requestItemRequestType          string
	requestItemRequestScopeType     string
	requestItemBibIdCode            string
	requestItemPickupLocationEnable bool
}

func reqField(m map[string]any, key NcipProperty, dst *string) error {
	v, ok := m[string(key)].(string)
	if !ok || v == "" {
		return fmt.Errorf("missing required NCIP configuration field: %s", key)
	}
	*dst = v
	return nil
}

func optField[T any](m map[string]any, key NcipProperty, dst *T, def T) {
	v, ok := m[string(key)].(T)
	if !ok {
		*dst = def
	} else {
		*dst = v
	}
}

func (l *LmsAdapterNcip) parseConfig(ncipInfo map[string]any) error {
	err := reqField(ncipInfo, Address, &l.address)
	if err != nil {
		return err
	}
	err = reqField(ncipInfo, FromAgency, &l.fromAgency)
	if err != nil {
		return err
	}
	optField(ncipInfo, FromAgencyAuthentication, &l.fromAgencyAuthentication, "")
	optField(ncipInfo, ToAgency, &l.toAgency, "default-to-agency")
	optField(ncipInfo, LookupUserEnable, &l.lookupUserEnable, true)
	optField(ncipInfo, AcceptItemEnable, &l.acceptItemEnable, true)
	optField(ncipInfo, CheckInItemEnable, &l.checkInItemEnable, true)
	optField(ncipInfo, CheckOutItemEnable, &l.checkOutItemEnable, true)
	optField(ncipInfo, RequestItemRequestType, &l.requestItemRequestType, "Page")
	optField(ncipInfo, RequestItemRequestScopeType, &l.requestItemRequestScopeType, "Item")
	optField(ncipInfo, RequestItemBibIdCode, &l.requestItemBibIdCode, "SYSNUMBER")
	optField(ncipInfo, RequestItemPickupLocationEnable, &l.requestItemPickupLocationEnable, true)
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
	if !l.lookupUserEnable {
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
	if !l.acceptItemEnable {
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
	borrowerBarcode string,
	pickupLocation string,
	itemLocation string,
) error {
	var pickupLocationField *ncip.SchemeValuePair
	if l.requestItemPickupLocationEnable && pickupLocation != "" {
		pickupLocationField = &ncip.SchemeValuePair{Text: pickupLocation}
	}
	var userIdField *ncip.UserId
	if borrowerBarcode != "" {
		userIdField = &ncip.UserId{UserIdentifierValue: borrowerBarcode}
	}
	bibIdField := ncip.BibliographicId{
		BibliographicRecordId: &ncip.BibliographicRecordId{
			BibliographicRecordIdentifier:     itemId,
			BibliographicRecordIdentifierCode: &ncip.SchemeValuePair{Text: l.requestItemBibIdCode},
		}}
	requestScopeTypeField := ncip.SchemeValuePair{Text: l.requestItemRequestScopeType}

	requestTypeField := ncip.SchemeValuePair{Text: l.requestItemRequestType}
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
	if !l.checkInItemEnable {
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
	borrowerBarcode string,
	externalReferenceValue string,
) error {
	if !l.checkOutItemEnable {
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
		UserId:    &ncip.UserId{UserIdentifierValue: borrowerBarcode},
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
