package ncipclient

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/ncip"
)

type NcipClientImpl struct {
	disableNamespace         bool
	client                   *http.Client
	address                  string
	fromAgency               string
	toAgency                 string
	fromAgencyAuthentication string
	logFunc                  NcipLogFunc
}

func NewNcipClient(client *http.Client, address string, fromAgency string, toAgency string, fromAgencyAuthentication string, disableNamespace bool) NcipClient {
	return &NcipClientImpl{
		disableNamespace:         disableNamespace,
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

func (n *NcipClientImpl) LookupUser(lookup ncip.LookupUser) (response *ncip.LookupUserResponse, err error) {
	lookup.InitiationHeader = n.prepareHeader(lookup.InitiationHeader)

	ncipMessage := &ncip.NCIPMessage{
		LookupUser: &lookup,
	}
	var ncipResponse *ncip.NCIPMessage
	defer func() { n.logOperation(ncipMessage, ncipResponse, err) }()
	ncipResponse, err = n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response = ncipResponse.LookupUserResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing LookupUserResponse")
	}
	err = n.checkProblem("NCIP user lookup", response.Problem)
	return response, err
}

func (n *NcipClientImpl) AcceptItem(accept ncip.AcceptItem) (response *ncip.AcceptItemResponse, err error) {
	accept.InitiationHeader = n.prepareHeader(accept.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		AcceptItem: &accept,
	}
	var ncipResponse *ncip.NCIPMessage
	defer func() { n.logOperation(ncipMessage, ncipResponse, err) }()
	ncipResponse, err = n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response = ncipResponse.AcceptItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing AcceptItemResponse")
	}
	err = n.checkProblem("NCIP accept item", response.Problem)
	return response, err
}

func (n *NcipClientImpl) DeleteItem(delete ncip.DeleteItem) (response *ncip.DeleteItemResponse, err error) {
	delete.InitiationHeader = n.prepareHeader(delete.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		DeleteItem: &delete,
	}
	var ncipResponse *ncip.NCIPMessage
	defer func() { n.logOperation(ncipMessage, ncipResponse, err) }()
	ncipResponse, err = n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response = ncipResponse.DeleteItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing DeleteItemResponse")
	}
	err = n.checkProblem("NCIP delete item", response.Problem)
	return response, err
}

func (n *NcipClientImpl) RequestItem(request ncip.RequestItem) (response *ncip.RequestItemResponse, err error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		RequestItem: &request,
	}
	var ncipResponse *ncip.NCIPMessage
	defer func() { n.logOperation(ncipMessage, ncipResponse, err) }()
	ncipResponse, err = n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response = ncipResponse.RequestItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing RequestItemResponse")
	}
	err = n.checkProblem("NCIP request item", response.Problem)
	return response, err
}

func (n *NcipClientImpl) CancelRequestItem(request ncip.CancelRequestItem) (response *ncip.CancelRequestItemResponse, err error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CancelRequestItem: &request,
	}
	var ncipResponse *ncip.NCIPMessage
	defer func() { n.logOperation(ncipMessage, ncipResponse, err) }()
	ncipResponse, err = n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response = ncipResponse.CancelRequestItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing CancelRequestItemResponse")
	}
	err = n.checkProblem("NCIP cancel request item", response.Problem)
	return response, err
}

func (n *NcipClientImpl) CheckInItem(request ncip.CheckInItem) (response *ncip.CheckInItemResponse, err error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CheckInItem: &request,
	}
	var ncipResponse *ncip.NCIPMessage
	defer func() { n.logOperation(ncipMessage, ncipResponse, err) }()
	ncipResponse, err = n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response = ncipResponse.CheckInItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing CheckInItemResponse")
	}
	err = n.checkProblem("NCIP check in item", response.Problem)
	return response, err
}

func (n *NcipClientImpl) CheckOutItem(request ncip.CheckOutItem) (response *ncip.CheckOutItemResponse, err error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)
	ncipMessage := &ncip.NCIPMessage{
		CheckOutItem: &request,
	}
	var ncipResponse *ncip.NCIPMessage
	defer func() { n.logOperation(ncipMessage, ncipResponse, err) }()
	ncipResponse, err = n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response = ncipResponse.CheckOutItemResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing CheckOutItemResponse")
	}
	// The XSD decoder represents malformed dates as zero; they do not undo checkout.
	if response.DateDue != nil && (response.DateDue.IsZero() || response.DateDue.Year() < 1) {
		response.DateDue = nil
	}
	err = n.checkProblem("NCIP check out item", response.Problem)
	return response, err
}

func (n *NcipClientImpl) CreateUserFiscalTransaction(request ncip.CreateUserFiscalTransaction) (response *ncip.CreateUserFiscalTransactionResponse, err error) {
	request.InitiationHeader = n.prepareHeader(request.InitiationHeader)

	ncipMessage := &ncip.NCIPMessage{
		CreateUserFiscalTransaction: &request,
	}
	var ncipResponse *ncip.NCIPMessage
	defer func() { n.logOperation(ncipMessage, ncipResponse, err) }()
	ncipResponse, err = n.sendReceiveMessage(ncipMessage)
	if err != nil {
		return nil, err
	}
	response = ncipResponse.CreateUserFiscalTransactionResponse
	if response == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing CreateUserFiscalTransactionResponse")
	}
	err = n.checkProblem("NCIP create user fiscal transaction", response.Problem)
	return response, err
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
		n.address, message, &respMessage, n.marshal, n.unmarshal)
	if err != nil {
		return &respMessage, fmt.Errorf("NCIP message exchange failed: %s", err.Error())
	}
	if len(respMessage.Problem) > 0 {
		return &respMessage, &NcipError{
			Message: "NCIP message processing failed",
			Problem: respMessage.Problem[0],
		}
	}
	return &respMessage, nil
}

func (n *NcipClientImpl) logOperation(outgoingMessage *ncip.NCIPMessage, incomingMessage *ncip.NCIPMessage, operationErr error) {
	if n.logFunc == nil {
		return
	}

	outgoing, outgoingErr := common.StructToMap(outgoingMessage)
	hideSensitive(outgoing)

	var incoming map[string]any
	var incomingErr error
	if incomingMessage != nil {
		incoming, incomingErr = common.StructToMap(incomingMessage)
		hideSensitive(incoming)
	}

	logErr := operationErr
	if logErr == nil {
		logErr = outgoingErr
	}
	if logErr == nil {
		logErr = incomingErr
	}
	n.logFunc(outgoing, incoming, logErr)
}

func hideSensitive(message map[string]any) {
	traverse(message, 0)
}

// removes values from the FromAgencyAuthentication and FromSystemAuthentication fields
// as well as user name/address information and AuthenticationInput fields except
// if type is "username"
func traverse(value any, level int) {
	if level > 20 {
		return
	}
	level++
	switch v := value.(type) {
	case map[string]any:
		if authenticationType, ok := v["AuthenticationInputType"].(map[string]any); ok {
			if authenticationType["#text"] != "username" {
				if _, exists := v["AuthenticationInputData"]; exists {
					v["AuthenticationInputData"] = "***"
				}
			}
		}
		for key, field := range v {
			if key == "FromAgencyAuthentication" || key == "FromSystemAuthentication" ||
				key == "NameInformation" || key == "UserAddressInformation" {
				v[key] = "***"
				continue
			}
			traverse(field, level)
		}
	case []any:
		for _, item := range v {
			traverse(item, level)
		}
	}
}

// transformNamespace handles namespace-free NCIP integrations while keeping
// the strongly typed NCIP model and all text/attribute values intact.
func transformNamespace(data []byte, remove bool) ([]byte, error) {
	const ns = "http://www.niso.org/2008/ncip"
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if remove && t.Name.Space == ns {
				t.Name.Space = ""
			} else if !remove && t.Name.Space == "" {
				t.Name.Space = ns
			}
			attrs := t.Attr[:0]
			for _, a := range t.Attr {
				if a.Name.Local == "xmlns" || a.Name.Space == "xmlns" {
					continue
				}
				if remove && a.Name.Space == ns {
					a.Name.Space = ""
				}
				attrs = append(attrs, a)
			}
			t.Attr = attrs
			token = t
		case xml.EndElement:
			if remove && t.Name.Space == ns {
				t.Name.Space = ""
			} else if !remove && t.Name.Space == "" {
				t.Name.Space = ns
			}
			token = t
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (n *NcipClientImpl) marshal(v any) ([]byte, error) {
	b, err := xml.Marshal(v)
	if err != nil || !n.disableNamespace {
		return b, err
	}
	return transformNamespace(b, true)
}

func (n *NcipClientImpl) unmarshal(b []byte, v any) error {
	if n.disableNamespace {
		var err error
		b, err = transformNamespace(b, false)
		if err != nil {
			return err
		}
	}
	return xml.Unmarshal(b, v)
}
