package adapter

import "github.com/indexdata/crosslink/ncip"

// NcipAdapter defines the interface for NCIP operations
// customData is from the DirectoryEntry.CustomData field
type NcipAdapter interface {
	AuthenticateUser(customData map[string]any, lookup ncip.LookupItem) (bool, error)
	AcceptItem(customData map[string]any, lookup ncip.AcceptItem) (bool, error)
	CheckOutItem(customData map[string]any, lookup ncip.CheckOutItem) error
	CheckInItem(customData map[string]any, lookup ncip.CheckInItem) error
	DeleteItem(customData map[string]any, lookup ncip.DeleteItem) error
	RequestItem(customData map[string]any, lookup ncip.RequestItem) error
}
