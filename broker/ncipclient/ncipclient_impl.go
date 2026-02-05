package ncipclient

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"reflect"

	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/ncip"
)

type NcipClientImpl struct {
	client                   *http.Client
	address                  string
	fromAgency               string
	toAgency                 string
	fromAgencyAuthentication string
	logFunc                  NcipLogFunc
}

func NewNcipClient(client *http.Client, address string, fromAgency string, toAgency string, fromAgencyAuthentication string) NcipClient {
	return &NcipClientImpl{
		client:                   client,
		address:                  address,
		fromAgency:               fromAgency,
		toAgency:                 toAgency,
		fromAgencyAuthentication: fromAgencyAuthentication,
	}
}

func (n *NcipClientImpl) SetLogFunc(logFunc NcipLogFunc) {
	n.logFunc = logFunc
}

func (n *NcipClientImpl) LookupUser(lookup ncip.LookupUser) (*ncip.LookupUserResponse, error) {
	lookup.InitiationHeader = n.prepareHeader(lookup.InitiationHeader)

	ncipMessage := &ncip.NCIPMessage{
		LookupUser: &lookup,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response := ncipResponse.LookupUserResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing LookupUserResponse")
	}
	return response, n.checkProblem("NCIP user lookup", response.Problem)
}

func (n *NcipClientImpl) AcceptItem(accept ncip.AcceptItem) (*ncip.AcceptItemResponse, error) {
	accept.InitiationHeader = n.prepareHeader(accept.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		AcceptItem: &accept,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response := ncipResponse.AcceptItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing AcceptItemResponse")
	}
	return response, n.checkProblem("NCIP accept item", response.Problem)
}

func (n *NcipClientImpl) DeleteItem(delete ncip.DeleteItem) (*ncip.DeleteItemResponse, error) {
	delete.InitiationHeader = n.prepareHeader(delete.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		DeleteItem: &delete,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response := ncipResponse.DeleteItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing DeleteItemResponse")
	}
	return response, n.checkProblem("NCIP delete item", response.Problem)
}

func (n *NcipClientImpl) RequestItem(request ncip.RequestItem) (*ncip.RequestItemResponse, error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		RequestItem: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response := ncipResponse.RequestItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing RequestItemResponse")
	}
	return response, n.checkProblem("NCIP request item", response.Problem)
}

func (n *NcipClientImpl) CancelRequestItem(request ncip.CancelRequestItem) (*ncip.CancelRequestItemResponse, error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CancelRequestItem: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response := ncipResponse.CancelRequestItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing CancelRequestItemResponse")
	}
	return response, n.checkProblem("NCIP cancel request item", response.Problem)
}

func (n *NcipClientImpl) CheckInItem(request ncip.CheckInItem) (*ncip.CheckInItemResponse, error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CheckInItem: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response := ncipResponse.CheckInItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing CheckInItemResponse")
	}
	return response, n.checkProblem("NCIP check in item", response.Problem)
}

func (n *NcipClientImpl) CheckOutItem(request ncip.CheckOutItem) (*ncip.CheckOutItemResponse, error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CheckOutItem: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response := ncipResponse.CheckOutItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing CheckOutItemResponse")
	}
	return response, n.checkProblem("NCIP check out item", response.Problem)
}

func (n *NcipClientImpl) CreateUserFiscalTransaction(request ncip.CreateUserFiscalTransaction) (*ncip.CreateUserFiscalTransactionResponse, error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)

	ncipMessage := &ncip.NCIPMessage{
		CreateUserFiscalTransaction: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response := ncipResponse.CreateUserFiscalTransactionResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing CreateUserFiscalTransactionResponse")
	}
	return response, n.checkProblem("NCIP create user fiscal transaction", response.Problem)
}

func (n *NcipClientImpl) checkProblem(op string, responseProblems []ncip.Problem) error {
	if len(responseProblems) > 0 {
		return &NcipError{
			Message: op + " failed",
			Problem: responseProblems[0],
		}
	}
	return nil
}

func (n *NcipClientImpl) prepareHeader(header *ncip.InitiationHeader) *ncip.InitiationHeader {
	if header == nil {
		header = &ncip.InitiationHeader{}
	}
	header.FromAgencyId.AgencyId = ncip.SchemeValuePair{
		Text: n.fromAgency,
	}
	header.ToAgencyId.AgencyId = ncip.SchemeValuePair{
		Text: n.toAgency,
	}
	header.FromAgencyAuthentication = n.fromAgencyAuthentication
	return header
}

func (n *NcipClientImpl) sendReceiveMessage(message *ncip.NCIPMessage) (*ncip.NCIPMessage, error) {
	if n.address == "" {
		return nil, fmt.Errorf("missing NCIP address in configuration")
	}
	message.Version = ncip.NCIP_V2_02_XSD

	var respMessage ncip.NCIPMessage

	err := httpclient.NewClient().RequestResponse(n.client, http.MethodPost, []string{httpclient.ContentTypeApplicationXml},
		n.address, message, &respMessage, xml.Marshal, xml.Unmarshal)
	if n.logFunc != nil {
		hideSensitive(message)
		var outgoing []byte
		var err1 error
		outgoing, err1 = xml.MarshalIndent(message, "", "  ")

		hideSensitive(&respMessage)
		var incoming []byte
		var err2 error
		incoming, err2 = xml.MarshalIndent(&respMessage, "", "  ")

		logErr := err
		if logErr == nil {
			logErr = err1
		}
		if logErr == nil {
			logErr = err2
		}
		n.logFunc(outgoing, incoming, logErr)
	}
	if err != nil {
		return nil, fmt.Errorf("NCIP message exchange failed: %s", err.Error())
	}
	if len(respMessage.Problem) > 0 {
		return nil, &NcipError{
			Message: "NCIP message processing failed",
			Problem: respMessage.Problem[0],
		}
	}
	return &respMessage, nil
}

func hideSensitive(message *ncip.NCIPMessage) {
	traverse(reflect.ValueOf(message), 0)
}

// removes values from the FromAgencyAuthentication and FromSystemAuthentication fields
// as well as AuthenticationInput fields except if type is "username"
func traverse(v reflect.Value, level int) {
	if level > 20 {
		return
	}
	level = level + 1
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		traverse(v.Elem(), level)
		return
	}
	if v.Kind() == reflect.Slice {
		if v.IsNil() {
			return
		}
		for i := 0; i < v.Len(); i++ {
			traverse(v.Index(i), level)
		}
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	if t == reflect.TypeOf(ncip.AuthenticationInput{}) {
		ncipAuthenticationInput := v.Interface().(ncip.AuthenticationInput)
		exclude := ncipAuthenticationInput.AuthenticationInputType.Text != "username"
		if exclude {
			ncipAuthenticationInput.AuthenticationInputData = "***"
			v.Set(reflect.ValueOf(ncipAuthenticationInput))
		}
		return
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Type.Kind() == reflect.String &&
			(field.Name == "FromAgencyAuthentication" || field.Name == "FromSystemAuthentication") {
			v.Field(i).SetString("***")
		} else {
			traverse(v.Field(i), level)
		}
	}
}
