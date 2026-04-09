package iso18626

import "encoding/xml"

const IllV1_2 = "1.2"

func NewISO18626Message() *ISO18626Message {
	msg := ISO18626Message{}
	msg.XMLName = xml.Name{Space: TARGET_NAMESPACE, Local: ROOT_TAG}
	msg.Version = IllV1_2
	return &msg
}
