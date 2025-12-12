package lms

import (
	"fmt"
	"net/http"

	"github.com/indexdata/crosslink/broker/ncipclient"
	"github.com/indexdata/crosslink/ncip"
)

// NCIP LMS Adapter, based on:
// https://github.com/openlibraryenvironment/mod-rs/blob/master/service/grails-app/services/org/olf/rs/hostlms/BaseHostLMSService.groovy

type LmsAdapterNcip struct {
	ncipInfo   map[string]any
	ncipClient ncipclient.NcipClient
}

func CreateLmsAdapterNcip(ncipInfo map[string]any) (LmsAdapter, error) {
	nc := ncipclient.NewNcipClient(http.DefaultClient, ncipInfo)
	return &LmsAdapterNcip{
		ncipInfo:   ncipInfo,
		ncipClient: nc,
	}, nil
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
	elements := []ncip.SchemeValuePair{{Text: "User Id"}}
	arg = ncip.LookupUser{
		AuthenticationInput: authenticationInput,
		UserElementType:     elements,
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
	arg := ncip.AcceptItem{
		UserId: &ncip.UserId{UserIdentifierValue: userId},
		ItemId: &ncip.ItemId{ItemIdentifierValue: itemId},
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
	arg := ncip.RequestItem{
		ItemId: []ncip.ItemId{{ItemIdentifierValue: itemId}},
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
	arg := ncip.CheckInItem{
		ItemId: ncip.ItemId{ItemIdentifierValue: itemId},
	}
	_, err := l.ncipClient.CheckInItem(arg)
	return err
}

func (l *LmsAdapterNcip) CheckOutItem(
	requestId string,
	itemBarcode string,
	borrowerBarcode string,
	externalReferenceValue string,
) error {
	arg := ncip.CheckOutItem{
		RequestId: &ncip.RequestId{RequestIdentifierValue: requestId},
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
