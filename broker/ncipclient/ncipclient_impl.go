package ncipclient

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/ncip"
)

type NcipClientImpl struct {
	client                   *http.Client
	address                  string
	fromAgency               string
	toAgency                 string
	fromAgencyAuthentication string
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
	from_agency := n.fromAgency
	if from_agency == "" {
		from_agency = "default-from-agency"
	}
	header.FromAgencyId.AgencyId = ncip.SchemeValuePair{
		Text: from_agency,
	}
	to_agency := n.toAgency
	if to_agency == "" {
		to_agency = "default-to-agency"
	}
	header.ToAgencyId.AgencyId = ncip.SchemeValuePair{
		Text: to_agency,
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
