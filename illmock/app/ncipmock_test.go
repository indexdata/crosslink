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

func TestPostMissingVersion(t *testing.T) {
	req := ncip.NCIPMessage{}
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
	assert.NotNil(t, ncipResponse.Problem)
	assert.Len(t, ncipResponse.Problem, 1)
	assert.Equal(t, string(ncip.MissingVersion), ncipResponse.Problem[0].ProblemType.Text)
}

func TestPostMissingMessageType(t *testing.T) {
	req := ncip.NCIPMessage{Version: ncip.NCIP_V2_02_XSD}
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
	assert.NotNil(t, ncipResponse.Problem)
	assert.Len(t, ncipResponse.Problem, 1)
	assert.Equal(t, string(ncip.MissingVersion), ncipResponse.Problem[0].ProblemType.Text)
}

func TestPostLookupUserMissingUserId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version:    ncip.NCIP_V2_02_XSD,
		LookupUser: &ncip.LookupUser{},
	}
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
	assert.NotNil(t, ncipResponse.DeleteItemResponse)
	assert.Len(t, ncipResponse.DeleteItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownItem), ncipResponse.DeleteItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "fitem-001", ncipResponse.DeleteItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestItemOK(t *testing.T) {
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
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 0)
}

func TestPostRequestMissingRequestType(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			ItemId: []ncip.ItemId{{
				ItemIdentifierValue: "item-001",
			}},
			RequestScopeType: ncip.SchemeValuePair{
				Text: "Bibliographic",
			},
		},
	}
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
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "RequestType is required", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestMissingRequestScopeType(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
			ItemId: []ncip.ItemId{{
				ItemIdentifierValue: "item-001",
			}},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
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
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "RequestScopeType is required", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}

func TestPostRequestItemFailItemId(t *testing.T) {
	req := ncip.NCIPMessage{
		Version: ncip.NCIP_V2_02_XSD,
		RequestItem: &ncip.RequestItem{
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
			RequestScopeType: ncip.SchemeValuePair{
				Text: "Bibliographic",
			},
			RequestType: ncip.SchemeValuePair{
				Text: "Hold",
			},
		},
	}
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
	assert.NotNil(t, ncipResponse.RequestItemResponse)
	assert.Len(t, ncipResponse.RequestItemResponse.Problem, 1)
	assert.Equal(t, string(ncip.UnknownUser), ncipResponse.RequestItemResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "f12345", ncipResponse.RequestItemResponse.Problem[0].ProblemDetail)
}
