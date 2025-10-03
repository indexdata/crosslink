package common

type BrokerMode string

const (
	BrokerModeOpaque      BrokerMode = "opaque"
	BrokerModeTransparent BrokerMode = "transparent"
	BrokerModeTranslucent BrokerMode = "translucent"
)

type Vendor string

const (
	VendorAlma    Vendor = "Alma"
	VendorReShare Vendor = "ReShare"
	VendorUnknown Vendor = "Unknown"
)

const DO_NOT_SEND = "doNotSend"
