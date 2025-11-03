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
	ncipInfo, ok := customData["ncip"].(map[string]any)
	if !ok {
		return false, fmt.Errorf("missing ncip configuration in customData")
	}
	mode, ok := ncipInfo["authuser_mode"].(string)
	if !ok {
		return false, fmt.Errorf("missing authuser_mode in ncip configuration")
	}
	switch mode {
	case "disabled":
		return true, nil
	case "manual":
		return false, nil
	case "auto":
		break
	default:
		return false, fmt.Errorf("unknown authuser_mode: %s", mode)
	}
	lookup.InitiationHeader = n.prepareHeader(ncipInfo, lookup.InitiationHeader)
	lookupUserResponse, err := n.lookupUser(ncipInfo, lookup)
	if err != nil {
		return false, err
	}
	if len(lookupUserResponse.Problem) > 0 {
		return false, &NcipError{
			Message: "NCIP user authentication failed",
			Problem: lookupUserResponse.Problem[0],
		}
	}
	return true, nil
}

func (n *NcipClientImpl) AcceptItem(customData map[string]any, arg ncip.AcceptItem) (bool, error) {
	return true, fmt.Errorf("NCIP AcceptItem not implemented")
}

func (n *NcipClientImpl) CheckOutItem(customData map[string]any, arg ncip.CheckOutItem) error {
	return fmt.Errorf("NCIP CheckOutItem not implemented")
}

func (n *NcipClientImpl) CheckInItem(customData map[string]any, arg ncip.CheckInItem) error {
	return fmt.Errorf("NCIP CheckInItem not implemented")
}

func (n *NcipClientImpl) DeleteItem(customData map[string]any, arg ncip.DeleteItem) error {
	return fmt.Errorf("NCIP DeleteItem not implemented")
}

func (n *NcipClientImpl) RequestItem(customData map[string]any, arg ncip.RequestItem) error {
	return fmt.Errorf("RequestItem not implemented")
}

func (n *NcipClientImpl) prepareHeader(ncipInfo map[string]any, header *ncip.InitiationHeader) *ncip.InitiationHeader {
	if header == nil {
		header = &ncip.InitiationHeader{}
	}
	header.FromAgencyId.AgencyId = ncip.SchemeValuePair{
		Text: ncipInfo["from_agency"].(string),
	}
	header.ToAgencyId.AgencyId = ncip.SchemeValuePair{
		Text: ncipInfo["to_agency"].(string),
	}
	if auth, ok := ncipInfo["from_agency_authentication"].(string); ok {
		header.FromAgencyAuthentication = auth
	}
	return header
}

func (n *NcipClientImpl) lookupUser(ncipInfo map[string]any, lookup ncip.LookupUser) (*ncip.LookupUserResponse, error) {
	ncipMessage := &ncip.NCIPMessage{
		LookupUser: &lookup,
	}
	ncipResponse, err := n.sendReceiveMessage(ncipInfo, ncipMessage)
	if err != nil {
		return nil, err
	}
	lookupUserResponse := ncipResponse.LookupUserResponse
	if lookupUserResponse == nil {
		return nil, fmt.Errorf("invalid NCIP response: missing LookupUserResponse")
	}
	return lookupUserResponse, nil
}

func (n *NcipClientImpl) sendReceiveMessage(ncipInfo map[string]any, message *ncip.NCIPMessage) (*ncip.NCIPMessage, error) {
	url, ok := ncipInfo["address"].(string)
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
