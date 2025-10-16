package app

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/assert"
)

var server *httptest.Server

func TestMain(m *testing.M) {
	server = httptest.NewServer(http.HandlerFunc(ncipMockHandler))
	exitCode := m.Run()
	server.Close()
	os.Exit(exitCode)
}

func TestGet(t *testing.T) {
	resp, err := http.Get(server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestPostNoMediaType(t *testing.T) {
	resp, err := http.Post(server.URL, "", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
	defer func() {
		dErr := resp.Body.Close()
		assert.NoError(t, dErr)
	}()
	buf, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Contains(t, string(buf), "mime: no media type")
}

func TestPostTextPlain(t *testing.T) {
	resp, err := http.Post(server.URL, "text/plain", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
	defer func() {
		dErr := resp.Body.Close()
		assert.NoError(t, dErr)
	}()
	buf, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Contains(t, string(buf), "unsupported media type")
}

func TestPostXml(t *testing.T) {
	resp, err := http.Post(server.URL, "application/xml;charset=utf-8", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	defer func() {
		dErr := resp.Body.Close()
		assert.NoError(t, dErr)
	}()
	buf, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	var ncipResponse ncip.NCIPMessage
	err = xml.Unmarshal(buf, &ncipResponse)
	assert.NoError(t, err)
	assert.NotNil(t, ncipResponse.Problem)
	assert.Len(t, ncipResponse.Problem, 1)
	assert.Equal(t, string(ncip.InvalidMessageSyntaxError), ncipResponse.Problem[0].ProblemType.Text)
}

func sendReceive(t *testing.T, req ncip.NCIPMessage) ncip.NCIPMessage {
	buf, err := xml.Marshal(req)
	assert.NoError(t, err)
	resp, err := http.Post(server.URL, "application/xml", bytes.NewReader(buf))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	defer func() {
		dErr := resp.Body.Close()
		assert.NoError(t, dErr)
	}()
	buf, err = io.ReadAll(resp.Body)
	assert.NoError(t, err)
	var ncipResponse ncip.NCIPMessage
	err = xml.Unmarshal(buf, &ncipResponse)
	assert.NoError(t, err)
	return ncipResponse
}

func TestPostMissingVersion(t *testing.T) {
	req := ncip.NCIPMessage{}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.Problem)
	assert.Len(t, ncipResponse.Problem, 1)
	assert.Equal(t, string(ncip.MissingVersion), ncipResponse.Problem[0].ProblemType.Text)
}

func TestPostMissingMessageType(t *testing.T) {
	req := ncip.NCIPMessage{Version: ncip.NCIP_V2_02_XSD}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.Problem)
	assert.Len(t, ncipResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnsupportedService), ncipResponse.Problem[0].ProblemType.Text)
}

func TestPostLookupUserMissingVersion(t *testing.T) {
	req := ncip.NCIPMessage{
		LookupUser: &ncip.LookupUser{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.Problem)
	assert.Len(t, ncipResponse.Problem, 1)
	assert.Equal(t, string(ncip.MissingVersion), ncipResponse.Problem[0].ProblemType.Text)
}

func TestPostLookupUserMissingUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version:    ncip.NCIP_V2_02_XSD,
		LookupUser: &ncip.LookupUser{},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.LookupUserResponse)
	assert.Len(t, ncipResponse.LookupUserResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.LookupUserResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "UserId or AuthenticationInput is required", ncipResponse.LookupUserResponse.Problem[0].ProblemDetail)
}

func TestPostLookupUserOK(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		LookupUser: &ncip.LookupUser{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.LookupUserResponse)
	assert.Len(t, ncipResponse.LookupUserResponse.Problem, 0)
}

func TestPostLookupUserFakeFailed(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		LookupUser: &ncip.LookupUser{
			UserId: &ncip.UserId{
				UserIdentifierValue: "f12345",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.LookupUserResponse)
	assert.Len(t, ncipResponse.LookupUserResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownUser), ncipResponse.LookupUserResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "f12345", ncipResponse.LookupUserResponse.Problem[0].ProblemDetail)
}

func TestPostAcceptItemOK(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		AcceptItem: &ncip.AcceptItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
			RequestId: ncip.RequestId{
				RequestIdentifierValue: "req-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.AcceptItemResponse)
	assert.Len(t, ncipResponse.AcceptItemResponse.Problem, 0)
}

func TestPostAcceptItemMissingRequestId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		AcceptItem: &ncip.AcceptItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.AcceptItemResponse)
	assert.Len(t, ncipResponse.AcceptItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.AcceptItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "RequestId is required", ncipResponse.AcceptItemResponse.Problem[0].ProblemDetail)
}

func TestPostAcceptItemFailUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		AcceptItem: &ncip.AcceptItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "f12345",
			},
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
			RequestId: ncip.RequestId{
				RequestIdentifierValue: "req-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.AcceptItemResponse)
	assert.Len(t, ncipResponse.AcceptItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownUser), ncipResponse.AcceptItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "f12345", ncipResponse.AcceptItemResponse.Problem[0].ProblemDetail)
}

func TestPostAcceptItemFailItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		AcceptItem: &ncip.AcceptItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "fitem-001",
			},
			RequestId: ncip.RequestId{
				RequestIdentifierValue: "req-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.AcceptItemResponse)
	assert.Len(t, ncipResponse.AcceptItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownItem), ncipResponse.AcceptItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "fitem-001", ncipResponse.AcceptItemResponse.Problem[0].ProblemDetail)
}

func TestPostDeleteItemOK(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		DeleteItem: &ncip.DeleteItem{
			ItemId: ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.DeleteItemResponse)
	assert.Len(t, ncipResponse.DeleteItemResponse.Problem, 0)
}

func TestPostDeleteItemFailItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		DeleteItem: &ncip.DeleteItem{
			ItemId: ncip.ItemId{
				ItemIdentifierValue: "fitem-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.DeleteItemResponse)
	assert.Len(t, ncipResponse.DeleteItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownItem), ncipResponse.DeleteItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "fitem-001", ncipResponse.DeleteItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestItemOK(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: []ncip.ItemId{{
				ItemIdentifierValue: "item-001",
			}},
			RequestScopeType: ncip.SchemeValuePair{
				Text: "Bibliographic",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 0)
}

func TestPostRequestMissingUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			ItemId: []ncip.ItemId{{
				ItemIdentifierValue: "item-001",
			}},
			RequestScopeType: ncip.SchemeValuePair{
				Text: "Bibliographic",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "UserId or AuthenticationInput is required", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestMissingItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			RequestScopeType: ncip.SchemeValuePair{
				Text: "Bibliographic",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "BibliographicId or ItemId is required", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestMissingRequestType(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: []ncip.ItemId{{
				ItemIdentifierValue: "item-001",
			}},
			RequestScopeType: ncip.SchemeValuePair{
				Text: "Bibliographic",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "RequestType is required", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestMissingRequestScopeType(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: []ncip.ItemId{{
				ItemIdentifierValue: "item-001",
			}},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "RequestScopeType is required", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestItemFailItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: []ncip.ItemId{{
				ItemIdentifierValue: "fitem-001",
			}},
			RequestScopeType: ncip.SchemeValuePair{
				Text: "Bibliographic",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownItem), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "fitem-001", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestItemFailUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "f12345",
			},
			ItemId: []ncip.ItemId{{
				ItemIdentifierValue: "item-001",
			}},
			RequestScopeType: ncip.SchemeValuePair{
				Text: "Bibliographic",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownUser), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "f12345", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostCancelRequestItemOK(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CancelRequestItem: &ncip.CancelRequestItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CancelRequestItemResponse)
	assert.Len(t, ncipResponse.CancelRequestItemResponse.Problem, 0)
}

func TestPostCancelRequestItemMissingUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CancelRequestItem: &ncip.CancelRequestItem{
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CancelRequestItemResponse)
	assert.Len(t, ncipResponse.CancelRequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.CancelRequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "UserId or AuthenticationInput is required", ncipResponse.CancelRequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostCancelRequestItemMissingRequestId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CancelRequestItem: &ncip.CancelRequestItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CancelRequestItemResponse)
	assert.Len(t, ncipResponse.CancelRequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.CancelRequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "RequestId or ItemId is required", ncipResponse.CancelRequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostCancelRequestItemMissingRequestType(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CancelRequestItem: &ncip.CancelRequestItem{
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CancelRequestItemResponse)
	assert.Len(t, ncipResponse.CancelRequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.CancelRequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "RequestType is required", ncipResponse.CancelRequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostCancelRequestItemFailUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CancelRequestItem: &ncip.CancelRequestItem{
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
			UserId: &ncip.UserId{
				UserIdentifierValue: "f12345",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CancelRequestItemResponse)
	assert.Len(t, ncipResponse.CancelRequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownUser), ncipResponse.CancelRequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "f12345", ncipResponse.CancelRequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostCancelRequestItemFailItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CancelRequestItem: &ncip.CancelRequestItem{
			ItemId: &ncip.ItemId{
				ItemIdentifierValue: "fitem-001",
			},
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CancelRequestItemResponse)
	assert.Len(t, ncipResponse.CancelRequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownItem), ncipResponse.CancelRequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "fitem-001", ncipResponse.CancelRequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostCheckInItemOK(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CheckInItem: &ncip.CheckInItem{
			ItemId: ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CheckInItemResponse)
	assert.Len(t, ncipResponse.CheckInItemResponse.Problem, 0)
}

func TestPostCheckInItemMissingItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version:     ncip.NCIP_V2_02_XSD,
		CheckInItem: &ncip.CheckInItem{},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CheckInItemResponse)
	assert.Len(t, ncipResponse.CheckInItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.CheckInItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "ItemId is required", ncipResponse.CheckInItemResponse.Problem[0].ProblemDetail)
}

func TestPostCheckInItemFailItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CheckInItem: &ncip.CheckInItem{
			ItemId: ncip.ItemId{
				ItemIdentifierValue: "fitem-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CheckInItemResponse)
	assert.Len(t, ncipResponse.CheckInItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownItem), ncipResponse.CheckInItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "fitem-001", ncipResponse.CheckInItemResponse.Problem[0].ProblemDetail)
}

func TestPostCheckOutItemOK(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CheckOutItem: &ncip.CheckOutItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CheckOutItemResponse)
	assert.Len(t, ncipResponse.CheckOutItemResponse.Problem, 0)
}

func TestPostCheckOutItemMissingUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CheckOutItem: &ncip.CheckOutItem{
			ItemId: ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CheckOutItemResponse)
	assert.Len(t, ncipResponse.CheckOutItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.CheckOutItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "UserId or AuthenticationInput is required", ncipResponse.CheckOutItemResponse.Problem[0].ProblemDetail)
}

func TestPostCheckOutItemMissingItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CheckOutItem: &ncip.CheckOutItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CheckOutItemResponse)
	assert.Len(t, ncipResponse.CheckOutItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.CheckOutItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "ItemId is required", ncipResponse.CheckOutItemResponse.Problem[0].ProblemDetail)
}

func TestPostCheckOutFailUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CheckOutItem: &ncip.CheckOutItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "f12345",
			},
			ItemId: ncip.ItemId{
				ItemIdentifierValue: "item-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CheckOutItemResponse)
	assert.Len(t, ncipResponse.CheckOutItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownUser), ncipResponse.CheckOutItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "f12345", ncipResponse.CheckOutItemResponse.Problem[0].ProblemDetail)
}

func TestPostCheckOutFailItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CheckOutItem: &ncip.CheckOutItem{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			ItemId: ncip.ItemId{
				ItemIdentifierValue: "fitem-001",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CheckOutItemResponse)
	assert.Len(t, ncipResponse.CheckOutItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownItem), ncipResponse.CheckOutItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "fitem-001", ncipResponse.CheckOutItemResponse.Problem[0].ProblemDetail)
}

func TestPostCreateUserFiscalTransactionOK(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CreateUserFiscalTransaction: &ncip.CreateUserFiscalTransaction{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
			FiscalTransactionInformation: ncip.FiscalTransactionInformation{
				FiscalActionType: ncip.SchemeValuePair{Text: "Charge"},
				FiscalTransactionReferenceId: &ncip.FiscalTransactionReferenceId{
					FiscalTransactionIdentifierValue: "ft-001",
				},
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CreateUserFiscalTransactionResponse)
	assert.Len(t, ncipResponse.CreateUserFiscalTransactionResponse.Problem, 0)
}

func TestPostCreateUserFiscalTransactionMissingUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CreateUserFiscalTransaction: &ncip.CreateUserFiscalTransaction{
			FiscalTransactionInformation: ncip.FiscalTransactionInformation{
				FiscalActionType: ncip.SchemeValuePair{Text: "Charge"},
				FiscalTransactionReferenceId: &ncip.FiscalTransactionReferenceId{
					FiscalTransactionIdentifierValue: "ft-001",
				},
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CreateUserFiscalTransactionResponse)
	assert.Len(t, ncipResponse.CreateUserFiscalTransactionResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.CreateUserFiscalTransactionResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "UserId or AuthenticationInput is required", ncipResponse.CreateUserFiscalTransactionResponse.Problem[0].ProblemDetail)
}

func TestPostCreateUserFiscalTransactionMissingTransactionInformation(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CreateUserFiscalTransaction: &ncip.CreateUserFiscalTransaction{
			UserId: &ncip.UserId{
				UserIdentifierValue: "12345",
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CreateUserFiscalTransactionResponse)
	assert.Len(t, ncipResponse.CreateUserFiscalTransactionResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.CreateUserFiscalTransactionResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "FiscalTransactionInformation is required", ncipResponse.CreateUserFiscalTransactionResponse.Problem[0].ProblemDetail)
}

func TestPostCreateUserFiscalTransactionFailUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		CreateUserFiscalTransaction: &ncip.CreateUserFiscalTransaction{
			UserId: &ncip.UserId{
				UserIdentifierValue: "f12345",
			},
			FiscalTransactionInformation: ncip.FiscalTransactionInformation{
				FiscalActionType: ncip.SchemeValuePair{Text: "Charge"},
				FiscalTransactionReferenceId: &ncip.FiscalTransactionReferenceId{
					FiscalTransactionIdentifierValue: "ft-001",
				},
			},
		},
	}
	ncipResponse := sendReceive(t, req)
	assert.NotNil(t, ncipResponse.CreateUserFiscalTransactionResponse)
	assert.Len(t, ncipResponse.CreateUserFiscalTransactionResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownUser), ncipResponse.CreateUserFiscalTransactionResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "f12345", ncipResponse.CreateUserFiscalTransactionResponse.Problem[0].ProblemDetail)
}
