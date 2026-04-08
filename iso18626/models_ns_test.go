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

func TestIso18626MessageNSMarshalUnmarshal(t *testing.T) {
	msg := NewIso18626MessageNS()

	buf, err := xml.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	expected := fmt.Sprintf(
		`<ISO18626Message xmlns="%s" ill:version="%s" xmlns:ill="%s" xmlns:xsi="%s" xsi:schemaLocation="%s %s"></ISO18626Message>`,
		IllNs, IllV1_2, IllNs, XsiNs, IllNs, IllSl,
	)
	if diff := diffXML(expected, string(buf)); diff != "" {
		t.Fatalf("marshal output differs from sample XML (-sample +actual):\n%s", diff)
	}

	var got Iso18626MessageNS
	if err := xml.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Namespace == nil || got.Namespace.Value != IllNs {
		t.Fatalf("unexpected xmlns value: %+v", got.Namespace)
	}
	if got.NsIllPx == nil || got.NsIllPx.Value != IllNs {
		t.Fatalf("unexpected xmlns:ill value: %+v", got.NsIllPx)
	}
	if got.NsXsiPx == nil || got.NsXsiPx.Value != XsiNs {
		t.Fatalf("unexpected xmlns:xsi value: %+v", got.NsXsiPx)
	}
	expectedSchemaLocation := fmt.Sprintf("%s %s", IllNs, IllSl)
	if got.XsiSchemaLoc == nil || got.XsiSchemaLoc.Value != expectedSchemaLocation {
		t.Fatalf("unexpected xsi:schemaLocation value: %+v", got.XsiSchemaLoc)
	}
	if got.Version.Value != IllV1_2 {
		t.Fatalf("unexpected version value: %+v", got.Version)
	}
}

func TestIso18626MessageNSMarshalAfterJSONRoundtrip(t *testing.T) {
	msg := NewIso18626MessageNS()
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
				Scheme: utils.NewPrefixAttr("scheme", "RESHARE"),
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
	if got, _ := jsonPayload["@xmlns"].(string); got != IllNs {
		t.Fatalf("unexpected @xmlns: %v", jsonPayload["@xmlns"])
	}
	if got, _ := jsonPayload["@xmlns:ill"].(string); got != IllNs {
		t.Fatalf("unexpected @xmlns:ill: %v", jsonPayload["@xmlns:ill"])
	}
	if got, _ := jsonPayload["@xmlns:xsi"].(string); got != XsiNs {
		t.Fatalf("unexpected @xmlns:xsi: %v", jsonPayload["@xmlns:xsi"])
	}
	expectedSchemaLocationJSON := fmt.Sprintf("%s %s", IllNs, IllSl)
	if got, _ := jsonPayload["@xsi:schemaLocation"].(string); got != expectedSchemaLocationJSON {
		t.Fatalf("unexpected @xsi:schemaLocation: %v", jsonPayload["@xsi:schemaLocation"])
	}

	var roundtripped Iso18626MessageNS
	if err := json.Unmarshal(jsonBytes, &roundtripped); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if roundtripped.Namespace == nil || roundtripped.Namespace.Value != IllNs {
		t.Fatalf("roundtrip @xmlns not preserved: %+v", roundtripped.Namespace)
	}
	if roundtripped.NsIllPx == nil || roundtripped.NsIllPx.Value != IllNs {
		t.Fatalf("roundtrip @xmlns:ill not preserved: %+v", roundtripped.NsIllPx)
	}
	if roundtripped.NsXsiPx == nil || roundtripped.NsXsiPx.Value != XsiNs {
		t.Fatalf("roundtrip @xmlns:xsi not preserved: %+v", roundtripped.NsXsiPx)
	}
	if roundtripped.XsiSchemaLoc == nil || roundtripped.XsiSchemaLoc.Value != expectedSchemaLocationJSON {
		t.Fatalf("roundtrip @xsi:schemaLocation not preserved: %+v", roundtripped.XsiSchemaLoc)
	}
	if roundtripped.Version.Value != IllV1_2 {
		t.Fatalf("roundtrip @version not preserved: %+v", roundtripped.Version)
	}

	xmlBytes, err := xml.Marshal(&roundtripped)
	if err != nil {
		t.Fatalf("xml marshal after json roundtrip failed: %v", err)
	}
	xmlText := string(xmlBytes)

	if strings.Contains(xmlText, `ill:="`) {
		t.Fatalf("invalid QName generated for version attribute: %s", xmlText)
	}
	if !strings.Contains(xmlText, `ill:version="1.2"`) {
		t.Fatalf("version attribute not generated correctly: %s", xmlText)
	}
	if !strings.Contains(xmlText, `ill:scheme="RESHARE"`) {
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

	var parsedMsg Iso18626MessageNS
	if err := xml.Unmarshal(xmlBytes, &parsedMsg); err != nil {
		t.Fatalf("generated XML cannot be unmarshalled into Iso18626MessageNS: %v\n%s", err, xmlText)
	}
	if parsedMsg.Request == nil || parsedMsg.Request.ServiceInfo == nil || parsedMsg.Request.ServiceInfo.ServiceLevel == nil {
		t.Fatalf("serviceLevel missing after xml unmarshal: %+v", parsedMsg.Request)
	}
	scheme := parsedMsg.Request.ServiceInfo.ServiceLevel.Scheme
	if scheme == nil {
		t.Fatalf("serviceLevel scheme attribute missing after xml unmarshal")
	}
	if scheme.Value != "RESHARE" {
		t.Fatalf("unexpected serviceLevel scheme value: %+v", scheme)
	}
	if scheme.Name.Local != "scheme" {
		t.Fatalf("unexpected serviceLevel scheme local name: %+v", scheme.Name)
	}
	if scheme.Name.Space != IllNs {
		t.Fatalf("unexpected serviceLevel scheme namespace: %+v", scheme.Name)
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
