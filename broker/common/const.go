package common

type BrokerMode string

const (
	BrokerModeOpaque      BrokerMode = "opaque"
	BrokerModeTransparent BrokerMode = "transparent"
)

type Vendor string

const (
	VendorAlma      Vendor = "Alma"
	VendorReShare   Vendor = "ReShare"
	VendorCrosslink Vendor = "Crosslink"
	VendorUnknown   Vendor = "Unknown"
)

const DO_NOT_SEND = "doNotSend"
