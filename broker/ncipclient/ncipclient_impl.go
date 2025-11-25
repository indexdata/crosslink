package ncipclient

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/ncip"
)

type NcipClientImpl struct {
	client *http.Client
}

func CreateNcipClient(client *http.Client) NcipClient {
	return &NcipClientImpl{
		client: client,
	}
}

func (n *NcipClientImpl) LookupUser(customData map[string]any, lookup ncip.LookupUser) (bool, error) {
	ncipInfo, err := n.getNcipInfo(customData)
	if err != nil {
		return false, err
	}
	handle, ret, err := n.checkMode(ncipInfo, LookupUserMode)
	if handle {
		return ret, err
	}
	lookup.InitiationHeader = n.prepareHeader(ncipInfo, lookup.InitiationHeader)

	ncipMessage := &ncip.NCIPMessage{
		LookupUser: &lookup,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return false, err
	}
	lookupUserResponse := ncipResponse.LookupUserResponse
	if lookupUserResponse == nil {
		return false, fmt.Errorf("invalid NCIP response: missing LookupUserResponse")
	}
	return true, n.checkProblem("NCIP user lookup", lookupUserResponse.Problem)
}

func (n *NcipClientImpl) AcceptItem(customData map[string]any, accept ncip.AcceptItem) (bool, error) {
	ncipInfo, err := n.getNcipInfo(customData)
	if err != nil {
		return false, err
	}
	handle, ret, err := n.checkMode(ncipInfo, AcceptItemMode)
	if handle {
		return ret, err
	}
	accept.InitiationHeader = n.prepareHeader(ncipInfo, accept.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		AcceptItem: &accept,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return false, err
	}
	acceptItemResponse := ncipResponse.AcceptItemResponse
	if acceptItemResponse == nil {
		return false, fmt.Errorf("invalid NCIP response: missing AcceptItemResponse")
	}
	return true, n.checkProblem("NCIP accept item", acceptItemResponse.Problem)
}

func (n *NcipClientImpl) DeleteItem(customData map[string]any, delete ncip.DeleteItem) error {
	ncipInfo, err := n.getNcipInfo(customData)
	if err != nil {
		return err
	}
	delete.InitiationHeader = n.prepareHeader(ncipInfo, delete.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		DeleteItem: &delete,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return err
	}
	deleteItemResponse := ncipResponse.DeleteItemResponse
	if deleteItemResponse == nil {
		return fmt.Errorf("invalid NCIP response: missing DeleteItemResponse")
	}
	return n.checkProblem("NCIP delete item", deleteItemResponse.Problem)
}

func (n *NcipClientImpl) RequestItem(customData map[string]any, request ncip.RequestItem) (bool, error) {
	ncipInfo, err := n.getNcipInfo(customData)
	if err != nil {
		return false, err
	}
	handle, ret, err := n.checkMode(ncipInfo, RequestItemMode)
	if handle {
		return ret, err
	}
	request.InitiationHeader = n.prepareHeader(ncipInfo, request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		RequestItem: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return false, err
	}
	requestItemResponse := ncipResponse.RequestItemResponse
	if requestItemResponse == nil {
		return false, fmt.Errorf("invalid NCIP response: missing RequestItemResponse")
	}
	return true, n.checkProblem("NCIP request item", requestItemResponse.Problem)
}

func (n *NcipClientImpl) CancelRequestItem(customData map[string]any, request ncip.CancelRequestItem) error {
	ncipInfo, err := n.getNcipInfo(customData)
	if err != nil {
		return err
	}
	request.InitiationHeader = n.prepareHeader(ncipInfo, request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CancelRequestItem: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return err
	}
	cancelRequestItemResponse := ncipResponse.CancelRequestItemResponse
	if cancelRequestItemResponse == nil {
		return fmt.Errorf("invalid NCIP response: missing CancelRequestItemResponse")
	}
	return n.checkProblem("NCIP cancel request item", cancelRequestItemResponse.Problem)
}

func (n *NcipClientImpl) CheckInItem(customData map[string]any, request ncip.CheckInItem) error {
	ncipInfo, err := n.getNcipInfo(customData)
	if err != nil {
		return err
	}
	request.InitiationHeader = n.prepareHeader(ncipInfo, request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CheckInItem: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return err
	}
	checkInItemResponse := ncipResponse.CheckInItemResponse
	if checkInItemResponse == nil {
		return fmt.Errorf("invalid NCIP response: missing CheckInItemResponse")
	}
	return n.checkProblem("NCIP check in item", checkInItemResponse.Problem)
}

func (n *NcipClientImpl) CheckOutItem(customData map[string]any, request ncip.CheckOutItem) error {
	ncipInfo, err := n.getNcipInfo(customData)
	if err != nil {
		return err
	}
	request.InitiationHeader = n.prepareHeader(ncipInfo, request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CheckOutItem: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return err
	}
	checkOutItemResponse := ncipResponse.CheckOutItemResponse
	if checkOutItemResponse == nil {
		return fmt.Errorf("invalid NCIP response: missing CheckOutItemResponse")
	}
	return n.checkProblem("NCIP check out item", checkOutItemResponse.Problem)
}

func (n *NcipClientImpl) CreateUserFiscalTransaction(customData map[string]any, request ncip.CreateUserFiscalTransaction) (bool, error) {
	ncipInfo, err := n.getNcipInfo(customData)
	if err != nil {
		return false, err
	}
	handle, ret, err := n.checkMode(ncipInfo, CreateUserFiscalTransactionMode)
	if handle {
		return ret, err
	}
	request.InitiationHeader = n.prepareHeader(ncipInfo, request.InitiationHeader)

	ncipMessage := &ncip.NCIPMessage{
		CreateUserFiscalTransaction: &request,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return false, err
	}
	createUserFiscalTransactionResponse := ncipResponse.CreateUserFiscalTransactionResponse
	if createUserFiscalTransactionResponse == nil {
		return false, fmt.Errorf("invalid NCIP response: missing CreateUserFiscalTransactionResponse")
	}
	return true, n.checkProblem("NCIP create user fiscal transaction", createUserFiscalTransactionResponse.Problem)
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

func (n *NcipClientImpl) getNcipInfo(customData map[string]any) (map[string]any, error) {
	ncipInfo, ok := customData["ncip"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing ncip configuration in customData")
	}
	return ncipInfo, nil
}

func (n *NcipClientImpl) checkMode(ncipInfo map[string]any, key NcipProperty) (bool, bool, error) {
	mode, ok := ncipInfo[string(key)].(string)
	if !ok {
		return true, false, fmt.Errorf("missing %s in ncip configuration", key)
	}
	switch mode {
	case string(ModeDisabled):
		return true, true, nil
	case string(ModeManual):
		return true, false, nil
	case string(ModeAuto):
		break
	default:
		return true, false, fmt.Errorf("unknown value for %s: %s", key, mode)
	}
	return false, false, nil
}

func (n *NcipClientImpl) prepareHeader(ncipInfo map[string]any, header *ncip.InitiationHeader) *ncip.InitiationHeader {
	if header == nil {
		header = &ncip.InitiationHeader{}
	}
	from_agency, ok := ncipInfo[string(FromAgency)].(string)
	if !ok || from_agency == "" {
		from_agency = "default-from-agency"
	}
	header.FromAgencyId.AgencyId = ncip.SchemeValuePair{
		Text: from_agency,
	}
	to_agency, ok := ncipInfo[string(ToAgency)].(string)
	if !ok || to_agency == "" {
		to_agency = "default-to-agency"
	}
	header.ToAgencyId.AgencyId = ncip.SchemeValuePair{
		Text: to_agency,
	}
	if auth, ok := ncipInfo[string(FromAgencyAuthentication)].(string); ok {
		header.FromAgencyAuthentication = auth
	}
	return header
}

func (n *NcipClientImpl) sendReceiveMessage(ncipInfo map[string]any, message *ncip.NCIPMessage) (*ncip.NCIPMessage, error) {
	url, ok := ncipInfo[string(Address)].(string)
	if !ok {
		return nil, fmt.Errorf("missing NCIP address in customData")
	}
	message.Version = ncip.NCIP_V2_02_XSD

	var respMessage ncip.NCIPMessage
	err := httpclient.NewClient().RequestResponse(n.client, http.MethodPost, []string{httpclient.ContentTypeApplicationXml},
		url, message, &respMessage, xml.Marshal, xml.Unmarshal)
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
