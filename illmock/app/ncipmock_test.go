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
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
	assert.Nil(t, err)
	var ncipResponse ncip.NCIPMessage
	err = xml.Unmarshal(buf, &ncipResponse)
	assert.NoError(t, err)
	assert.NotNil(t, ncipResponse.LookupUserResponse)
	assert.Len(t, ncipResponse.LookupUserResponse.Problem, 1)
	assert.Equal(t, string(ncip.NeededDataMissing), ncipResponse.LookupUserResponse.Problem[0].ProblemType.Text)
	assert.Equal(t, "UserId or AuthenticationInput is required", ncipResponse.LookupUserResponse.Problem[0].ProblemDetail)
}
