package iso18626

import (
	utils "github.com/indexdata/go-utils/utils"
)

func InitNs() {
	utils.NSDefault("http://illtransactions.org/2013/iso18626")
	utils.NSPrefix("ill", "http://illtransactions.org/2013/iso18626")
	utils.NSPrefix("xsi", "http://www.w3.org/2001/XMLSchema-instance")
	utils.AttrDefault("schemaLocation", "http://illtransactions.org/2013/iso18626 http://illtransactions.org/schemas/ISO-18626-v1_2.xsd")
	utils.AttrDefault("version", "1.2")
}

type Iso18626MessageNS struct {
	Namespace *utils.PrefixAttr `xml:"xmlns,attr"`
	ISO18626Message
	NsIllPx      *utils.PrefixAttr `xml:"xmlns ill,attr"`
	NsXsiPx      *utils.PrefixAttr `xml:"xmlns xsi,attr"`
	XsiSchemaLoc *utils.PrefixAttr `xml:"http://www.w3.org/2001/XMLSchema-instance schemaLocation,attr"`
}
