package iso18626

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
	"time"

	utils "github.com/indexdata/go-utils/utils"
)

const IllNs = "http://illtransactions.org/2013/iso18626"
const XsiNs = "http://www.w3.org/2001/XMLSchema-instance"
const IllSl = "http://illtransactions.org/schemas/ISO-18626-v1_2.xsd"

func TestISO18626MessageMarshalUnmarshal(t *testing.T) {
	msg := NewISO18626Message()

	buf, err := xml.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	xmlText := string(buf)
	required := []string{
		`<ISO18626Message`,
		`xmlns:xsi="` + XsiNs + `"`,
		`xsi:schemaLocation="` + IllNs + ` ` + IllSl + `"`,
		`xmlns="` + IllNs + `"`,
		`xmlns:iso18626="` + IllNs + `"`,
		`iso18626:version="` + IllV1_2 + `"`,
	}
	for _, frag := range required {
		if !strings.Contains(xmlText, frag) {
			t.Fatalf("marshal output missing fragment %q:\n%s", frag, xmlText)
		}
	}

	var got ISO18626Message
	if err := xml.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Version != IllV1_2 {
		t.Fatalf("unexpected version value: %+v", got.Version)
	}
}

func TestISO18626MessageMarshalAfterJSONRoundtrip(t *testing.T) {
	schemeRs := "RESHARE"
	msg := NewISO18626Message()
	msg.Request = &Request{
		Header: Header{
			SupplyingAgencyId: TypeAgencyId{
				AgencyIdType:  TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "AU-MELBOURNE",
			},
			RequestingAgencyId: TypeAgencyId{
				AgencyIdType:  TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "AU-SYDNEY",
			},
			Timestamp:                 utils.XSDDateTime{Time: time.Date(2026, 4, 8, 18, 30, 11, 0, time.UTC)},
			RequestingAgencyRequestId: "SYD-357~norota",
		},
		BibliographicInfo: BibliographicInfo{
			Title: "jakub test",
		},
		ServiceInfo: &ServiceInfo{
			ServiceType: TypeServiceTypeLoan,
			ServiceLevel: &TypeSchemeValuePair{
				Text:   "standard",
				Scheme: &schemeRs,
			},
		},
	}

	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}
	var jsonPayload map[string]any
	if err := json.Unmarshal(jsonBytes, &jsonPayload); err != nil {
		t.Fatalf("json unmarshal for assertion failed: %v", err)
	}

	if got, _ := jsonPayload["@version"].(string); got != IllV1_2 {
		t.Fatalf("unexpected @version: %v", jsonPayload["@version"])
	}

	var roundtripped ISO18626Message
	if err := json.Unmarshal(jsonBytes, &roundtripped); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if roundtripped.Version != IllV1_2 {
		t.Fatalf("roundtrip @version not preserved: %+v", roundtripped.Version)
	}

	xmlBytes, err := xml.Marshal(&roundtripped)
	if err != nil {
		t.Fatalf("xml marshal after json roundtrip failed: %v", err)
	}
	xmlText := string(xmlBytes)

	if strings.Contains(xmlText, `iso18626:="`) {
		t.Fatalf("invalid QName generated for version attribute: %s", xmlText)
	}
	if !strings.Contains(xmlText, `iso18626:version="1.2"`) {
		t.Fatalf("version attribute not generated correctly: %s", xmlText)
	}
	if !strings.Contains(xmlText, `iso18626:scheme="RESHARE"`) {
		t.Fatalf("scheme attribute not generated correctly: %s", xmlText)
	}
	if strings.Contains(xmlText, `="RESHARE"`) && !strings.Contains(xmlText, `scheme="RESHARE"`) {
		t.Fatalf("scheme attribute has malformed name: %s", xmlText)
	}
	var parsed struct {
		XMLName xml.Name `xml:"ISO18626Message"`
	}
	if err := xml.Unmarshal(xmlBytes, &parsed); err != nil {
		t.Fatalf("generated XML is not parseable: %v\n%s", err, xmlText)
	}

	var parsedMsg ISO18626Message
	if err := xml.Unmarshal(xmlBytes, &parsedMsg); err != nil {
		t.Fatalf("generated XML cannot be unmarshalled into ISO18626Message: %v\n%s", err, xmlText)
	}
	if parsedMsg.Request == nil || parsedMsg.Request.ServiceInfo == nil || parsedMsg.Request.ServiceInfo.ServiceLevel == nil {
		t.Fatalf("serviceLevel missing after xml unmarshal: %+v", parsedMsg.Request)
	}
	scheme := parsedMsg.Request.ServiceInfo.ServiceLevel.Scheme
	if scheme == nil {
		t.Fatalf("serviceLevel scheme attribute missing after xml unmarshal")
	}
	if *scheme != "RESHARE" {
		t.Fatalf("unexpected serviceLevel scheme value: %+v", scheme)
	}
}

func diffXML(sampleXML, actualXML string) string {
	if sampleXML == actualXML {
		return ""
	}
	return lineDiff(sampleXML, actualXML)
}

func lineDiff(sampleXML, actualXML string) string {
	sampleLines := strings.Split(sampleXML, "\n")
	actualLines := strings.Split(actualXML, "\n")
	maxLines := len(sampleLines)
	if len(actualLines) > maxLines {
		maxLines = len(actualLines)
	}

	var b strings.Builder
	b.WriteString("--- sample\n+++ actual\n")
	for i := 0; i < maxLines; i++ {
		var s, a string
		if i < len(sampleLines) {
			s = sampleLines[i]
		}
		if i < len(actualLines) {
			a = actualLines[i]
		}
		if s == a {
			continue
		}
		fmt.Fprintf(&b, "-%d %s\n+%d %s\n", i+1, s, i+1, a)
	}
	return strings.TrimRight(b.String(), "\n")
}
