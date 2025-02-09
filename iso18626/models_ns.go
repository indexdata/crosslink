package iso18626

import (
	"encoding/xml"
	"fmt"

	utils "github.com/indexdata/go-utils/utils"
)

const IllNs = "http://illtransactions.org/2013/iso18626"
const XsiNs = "http://www.w3.org/2001/XMLSchema-instance"
const IllSl = "http://illtransactions.org/schemas/ISO-18626-v1_2.xsd"
const IllV1_2 = "1.2"

func InitNs() {
	utils.NSDefault(IllNs)
	utils.NSPrefix("ill", IllNs)
	utils.NSPrefix("xsi", XsiNs)
	utils.AttrDefault("schemaLocation", fmt.Sprintln(IllNs, IllSl))
	utils.AttrDefault("version", IllV1_2)
}

type Iso18626MessageNS struct {
	Namespace *utils.PrefixAttr `xml:"xmlns,attr"`
	ISO18626Message
	NsIllPx      *utils.PrefixAttr `xml:"xmlns ill,attr"`
	NsXsiPx      *utils.PrefixAttr `xml:"xmlns xsi,attr"`
	XsiSchemaLoc *utils.PrefixAttr `xml:"http://www.w3.org/2001/XMLSchema-instance schemaLocation,attr"`
}

func NewIso18626MessageNS() *Iso18626MessageNS {
	InitNs()
	msg := Iso18626MessageNS{}
	msg.Namespace = utils.NewPrefixAttr("xmlns", IllNs)
	msg.NsIllPx = NewPrefixAttrNS("xmlns", "ill", IllNs)
	msg.NsXsiPx = NewPrefixAttrNS("xmlns", "xsi", XsiNs)
	msg.XsiSchemaLoc = NewPrefixAttrNS(XsiNs, "schemaLocation", fmt.Sprintln(IllNs, IllSl))
	msg.ISO18626Message.Version = *NewPrefixAttrNS(IllNs, "version", IllV1_2)
	return &msg
}

// TODO move to go-utils
func NewPrefixAttrNS(ns string, name string, value string) *utils.PrefixAttr {
	return &utils.PrefixAttr{Attr: xml.Attr{Name: xml.Name{Space: ns, Local: name}, Value: value}}
}
