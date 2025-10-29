package adapter

import "github.com/indexdata/crosslink/ncip"

// implement the ncip interface

type NcipImplAdapter struct {
}

func CreateNcipImplAdapter() NcipAdapter {
	return &NcipImplAdapter{}
}

func (n *NcipImplAdapter) AuthenticateUser(customData map[string]any, lookup ncip.LookupItem) (bool, error) {
	return true, nil
}

func (n *NcipImplAdapter) AcceptItem(customData map[string]any, lookup ncip.AcceptItem) (bool, error) {
	return true, nil
}

func (n *NcipImplAdapter) CheckOutItem(customData map[string]any, lookup ncip.CheckOutItem) error {
	return nil
}

func (n *NcipImplAdapter) CheckInItem(customData map[string]any, lookup ncip.CheckInItem) error {
	return nil
}

func (n *NcipImplAdapter) DeleteItem(customData map[string]any, lookup ncip.DeleteItem) error {
	return nil
}

func (n *NcipImplAdapter) RequestItem(customData map[string]any, lookup ncip.RequestItem) error {
	return nil
}
