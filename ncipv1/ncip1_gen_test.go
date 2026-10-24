package ncipv1

import (
	"encoding/xml"
	"os"
	"reflect"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMarshalLookupUser(t *testing.T) {
	agency := UniqueAgencyId{Scheme: "urn:agency", Value: "supplier"}
	message := NCIPMessage{
		Version: NCIP_V1_0_DTD,
		LookupUser: &LookupUser{
			InitiationHeader: InitiationHeader{
				FromAgencyId: FromAgencyId{UniqueAgencyId: UniqueAgencyId{Scheme: "urn:agency", Value: "sender"}},
				ToAgencyId:   ToAgencyId{UniqueAgencyId: agency},
			},
			UniqueUserId: &UniqueUserId{UniqueAgencyId: agency, UserIdentifierValue: "test-user"},
			UserElementType: []UserElementType{
				{Scheme: "urn:user-elements", Value: "Name Information"},
				{Scheme: "urn:user-elements", Value: "User Address Information"},
			},
		},
	}
	data, err := xml.MarshalIndent(message, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(data)+"\n" != string(fixture(t, "lookup_user.xml")) {
		t.Fatalf("unexpected request XML:\n%s", data)
	}
}

func TestUnmarshalFixtures(t *testing.T) {
	for _, name := range []string{"lookup_user.xml", "checkout_response.xml", "checkin_error.xml"} {
		t.Run(name, func(t *testing.T) {
			var message NCIPMessage
			if err := xml.Unmarshal(fixture(t, name), &message); err != nil {
				t.Fatal(err)
			}
			if message.Version != NCIP_V1_0_DTD || message.XMLName != (xml.Name{Local: "NCIPMessage"}) {
				t.Fatalf("unexpected envelope: %v, %q", message.XMLName, message.Version)
			}
			switch name {
			case "lookup_user.xml":
				r := message.LookupUser
				if r == nil || r.UniqueUserId == nil || r.UniqueUserId.UserIdentifierValue != "test-user" || len(r.UserElementType) != 2 || r.VisibleUserId != nil {
					t.Fatalf("unexpected lookup request: %+v", r)
				}
			case "checkout_response.xml":
				r := message.CheckOutItemResponse
				if r == nil || r.Problem != nil || r.UniqueItemId == nil || r.UniqueItemId.ItemIdentifierValue != "5555555025805" || r.UniqueUserId == nil || r.UniqueUserId.UserIdentifierValue != "TNSNOTREAL" {
					t.Fatalf("unexpected checkout response: %+v", r)
				}
				if r.DateDue == nil || !r.DateDue.Equal(time.Date(2021, 6, 30, 16, 45, 0, 0, time.UTC)) {
					t.Fatalf("unexpected due date: %v", r.DateDue)
				}
			case "checkin_error.xml":
				r := message.CheckInItemResponse
				if r == nil || r.Problem == nil || len(r.Problem.ProcessingError) != 1 || r.UniqueItemId != nil {
					t.Fatalf("unexpected checkin response: %+v", r)
				}
				problem := r.Problem.ProcessingError[0]
				element := problem.ProcessingErrorElement.ProcessingErrorElement
				if problem.ProcessingErrorType.Value != "Item Not Checked Out" || element == nil || element.ElementName != "ItemIdentifierValue" || element.ProcessingErrorValue != "Item not on loan." {
					t.Fatalf("unexpected processing error: %+v", problem)
				}
			}
			data, err := xml.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			var roundTrip NCIPMessage
			if err := xml.Unmarshal(data, &roundTrip); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(message, roundTrip) {
				t.Fatal("XML round trip changed the message")
			}
		})
	}
}
