package sruapi

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/sru"
	"github.com/stretchr/testify/assert"
)

func TestProduceSurrogateDiagnostic(t *testing.T) {
	var api SruApi
	record := api.produceSurrogateDiagnostic(1, "message", "info:srw/diagnostic/1/60")
	assert.NotNil(t, record)
	assert.Equal(t, "info::srw/schema/1/diagnostics-v1.1", record.RecordSchema)
	assert.Contains(t, string(record.RecordData.StringOrXmlFragmentDefinition), "<uri>info:srw/diagnostic/1/60</uri>")
}

func getSr(t *testing.T, uri string) *sru.SearchRetrieveResponse {
	resp, err := http.Get(uri)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Contains(t, string(buf), "<searchRetrieveResponse")
	var sruResp sru.SearchRetrieveResponse
	err = xml.Unmarshal(buf, &sruResp)
	assert.Nil(t, err)
	assert.NotNil(t, sruResp.Version)
	return &sruResp
}

func getExplain(t *testing.T, uri string) *sru.ExplainResponse {
	resp, err := http.Get(uri)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Contains(t, string(buf), "<explainResponse")
	var expResp sru.ExplainResponse
	err = xml.Unmarshal(buf, &expResp)
	assert.Nil(t, err)
	assert.NotNil(t, expResp.Version)
	return &expResp
}

func TestSruService(t *testing.T) {

	api := CreateSruApi()
	server := httptest.NewServer(api.HttpHandler())
	defer server.Close()
	url := server.URL

	t.Run("cql ok", func(t *testing.T) {
		res, err := api.getIdFromQuery("id=1")
		assert.Nil(t, err)
		assert.Equal(t, "1", res)
	})

	t.Run("cql ok", func(t *testing.T) {
		res, err := api.getIdFromQuery("id==1")
		assert.Nil(t, err)
		assert.Equal(t, "1", res)
	})

	t.Run("cql syntax error 1", func(t *testing.T) {
		_, err := api.getIdFromQuery("id=")
		assert.NotNil(t, err)
		assert.Equal(t, "search term expected at position 3: id=̰", err.Error())
	})

	t.Run("cql bool", func(t *testing.T) {
		_, err := api.getIdFromQuery("a and b")
		assert.NotNil(t, err)
		assert.Equal(t, "missing search clause", err.Error())
	})

	t.Run("cql bad index", func(t *testing.T) {
		_, err := api.getIdFromQuery("title = a")
		assert.NotNil(t, err)
		assert.Equal(t, "unknown index: title", err.Error())
	})

	t.Run("cql bad relation", func(t *testing.T) {
		_, err := api.getIdFromQuery("id > a")
		assert.NotNil(t, err)
		assert.Equal(t, "unsupported relation: >", err.Error())
	})

	t.Run("bad method", func(t *testing.T) {
		resp, err := http.Post(url, "text/plain", strings.NewReader("hello"))
		assert.Nil(t, err)
		assert.Equal(t, 405, resp.StatusCode)
	})

	t.Run("sr1.1", func(t *testing.T) {
		sruResp := getSr(t, url+"?version=1.1&query=id%3D1")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 1, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, "info:srw/diagnostic/1/5", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Unsupported version", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("sr1.2", func(t *testing.T) {
		sruResp := getSr(t, url+"?version=1.2&query=id%3D1")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 1, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, "info:srw/diagnostic/1/5", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Unsupported version", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("sr2.0 no records", func(t *testing.T) {
		sruResp := getSr(t, url+"?query=id%3D1")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 0)
	})

	t.Run("sr2.0 bad maximumRecords", func(t *testing.T) {
		sruResp := getSr(t, url+"?version=2.0&query=id%3D1&maximumRecords=x")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 1)
		assert.Equal(t, "info:srw/diagnostic/1/6", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "maximumRecords", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("sr2.0 bad startRecord", func(t *testing.T) {
		sruResp := getSr(t, url+"?version=2.0&query=id%3D1&startRecord=x")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 1)
		assert.Equal(t, "info:srw/diagnostic/1/6", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "startRecord", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("sr2.0 with records", func(t *testing.T) {
		sruResp := getSr(t, url+"?version=2.0&query=id%3D1&maximumRecords=1")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 1)
		assert.Equal(t, "xml", string(*sruResp.Records.Record[0].RecordXMLEscaping))
		assert.Contains(t, string(sruResp.Records.Record[0].RecordData.StringOrXmlFragmentDefinition), "<subfield code=\"i\">1</subfield>")
	})

	t.Run("sr2.0 with surrogate diagnostic record", func(t *testing.T) {
		sruResp := getSr(t, url+"?version=2.0&query=id%3Dsd&maximumRecords=1")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 1)
		assert.Equal(t, "xml", string(*sruResp.Records.Record[0].RecordXMLEscaping))
		assert.Contains(t, string(sruResp.Records.Record[0].RecordData.StringOrXmlFragmentDefinition), "mock error")
	})

	t.Run("sr syntaxerror", func(t *testing.T) {
		sruResp := getSr(t, url+"?version=2.0&query=id")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 1, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, "info:srw/diagnostic/1/10", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Query syntax error", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("exp1.1", func(t *testing.T) {
		sruResp := getExplain(t, url+"?version=1.1")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 1, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, "info:srw/diagnostic/1/5", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Unsupported version", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("exp2.0", func(t *testing.T) {
		sruResp := getExplain(t, url+"?version=2.0")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
	})

}
