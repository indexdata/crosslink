package sruapi

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/marcxml"
	"github.com/indexdata/crosslink/sru"
	"github.com/stretchr/testify/assert"
)

func TestGetSurrogateDiagnostic(t *testing.T) {
	var api SruApi
	record := api.getSurrogateDiagnostic(1, "64", "Record temporarily unavailable", "x")
	assert.NotNil(t, record)
	assert.Equal(t, "info:srw/schema/1/diagnostics-v1.1", record.RecordSchema)
	assert.Contains(t, string(record.RecordData.XMLContent), "<uri>info:srw/diagnostic/1/64</uri>")
}

func getSr(t *testing.T, uri string) *sru.SearchRetrieveResponse {
	resp, err := http.Get(uri)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Contains(t, string(buf), "<searchRetrieveResponse")
	assert.Contains(t, string(buf), " xmlns=\"http://docs.oasis-open.org")
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
		res, diag := api.getIdFromQuery("id==1")
		assert.Nil(t, diag)
		assert.Equal(t, "1", res)
	})

	t.Run("cql syntax error 1", func(t *testing.T) {
		_, diag := api.getIdFromQuery("id=")
		assert.NotNil(t, diag)
		assert.Equal(t, "Query syntax error", diag.Message)
		assert.Equal(t, "search term expected at position 3: id=̰", diag.Details)
	})

	t.Run("cql bool", func(t *testing.T) {
		id, diag := api.getIdFromQuery("a and b")
		assert.Nil(t, diag)
		assert.Equal(t, "", id)
	})

	t.Run("cql bad index", func(t *testing.T) {
		_, diag := api.getIdFromQuery("title = a")
		assert.NotNil(t, diag)
		assert.Equal(t, "Unsupported index", diag.Message)
		assert.Equal(t, "title", diag.Details)
	})

	t.Run("cql unsupported relation", func(t *testing.T) {
		_, diag := api.getIdFromQuery("id > a")
		assert.NotNil(t, diag)
		assert.Equal(t, "Unsupported relation", diag.Message)
		assert.Equal(t, ">", diag.Details)
	})

	t.Run("bad method", func(t *testing.T) {
		resp, err := http.Post(url, "text/plain", strings.NewReader("hello"))
		assert.Nil(t, err)
		assert.Equal(t, 405, resp.StatusCode)
	})

	t.Run("sr1.1", func(t *testing.T) {
		cqlQuery := "id%3D1"
		sruResp := getSr(t, url+"?version=1.1&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 1)
		assert.Equal(t, "info:srw/diagnostic/1/5", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Unsupported version", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("sr1.2", func(t *testing.T) {
		cqlQuery := "id%3D1"
		sruResp := getSr(t, url+"?version=1.2&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 1)
		assert.Equal(t, "info:srw/diagnostic/1/5", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Unsupported version", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("sr2.0 no records", func(t *testing.T) {
		cqlQuery := "id%3D1"
		sruResp := getSr(t, url+"?query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 0)
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 0)
	})

	t.Run("sr2.0 bad maximumRecords", func(t *testing.T) {
		cqlQuery := "id%3D1"
		sruResp := getSr(t, url+"?version=2.0&maximumRecords=x&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 1)
		assert.Equal(t, "info:srw/diagnostic/1/6", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "maximumRecords", sruResp.Diagnostics.Diagnostic[0].Details)
	})

	t.Run("sr2.0 bad startRecord", func(t *testing.T) {
		cqlQuery := "id%3D1"
		sruResp := getSr(t, url+"?version=2.0&startRecord=x&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 1)
		assert.Equal(t, "info:srw/diagnostic/1/6", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "startRecord", sruResp.Diagnostics.Diagnostic[0].Details)
	})

	t.Run("sr2.0 with one holding", func(t *testing.T) {
		cqlQuery := "id%3D42"
		sruResp := getSr(t, url+"?version=2.0&maximumRecords=1&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 1)
		assert.Equal(t, "xml", string(*sruResp.Records.Record[0].RecordXMLEscaping))
		assert.Equal(t, "info:srw/schema/1/marcxml-v1.1", sruResp.Records.Record[0].RecordSchema)
		assert.Contains(t, string(sruResp.Records.Record[0].RecordData.XMLContent), "<subfield code=\"l\">42</subfield>")

		var marc marcxml.Record
		err := xml.Unmarshal([]byte(sruResp.Records.Record[0].RecordData.XMLContent), &marc)
		assert.Nil(t, err)
		assert.Equal(t, "42", marc.Id)
		matched := false
		for _, f := range marc.RecordType.Datafield {
			if f.Tag == "999" && f.Ind1 == "1" && f.Ind2 == "1" {
				matched = true
				assert.Len(t, f.Subfield, 2)
				assert.Equal(t, "l", f.Subfield[0].Code)
				assert.Equal(t, "42", string(f.Subfield[0].Text))
				assert.Equal(t, "s", f.Subfield[1].Code)
				assert.Equal(t, "isil:sup1", string(f.Subfield[1].Text))
			}
		}
		assert.True(t, matched)
	})

	t.Run("sr2.0 with two holdings", func(t *testing.T) {
		cqlQuery := "id%3D42%3B43"
		sruResp := getSr(t, url+"?version=2.0&maximumRecords=1&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 1)
		assert.Equal(t, "xml", string(*sruResp.Records.Record[0].RecordXMLEscaping))
		assert.Equal(t, "info:srw/schema/1/marcxml-v1.1", sruResp.Records.Record[0].RecordSchema)
		assert.Contains(t, string(sruResp.Records.Record[0].RecordData.XMLContent), "<subfield code=\"l\">42</subfield>")

		var marc marcxml.Record
		err := xml.Unmarshal([]byte(sruResp.Records.Record[0].RecordData.XMLContent), &marc)
		assert.Nil(t, err)
		assert.Equal(t, "42;43", marc.Id)
		matched := 0
		for _, f := range marc.RecordType.Datafield {
			if f.Tag == "999" && f.Ind1 == "1" && f.Ind2 == "1" {
				assert.Len(t, f.Subfield, 2)
				if matched == 0 {
					assert.Equal(t, "l", f.Subfield[0].Code)
					assert.Equal(t, "42", string(f.Subfield[0].Text))
					assert.Equal(t, "s", f.Subfield[1].Code)
					assert.Equal(t, "isil:sup1", string(f.Subfield[1].Text))
				} else if matched == 1 {
					assert.Equal(t, "l", f.Subfield[0].Code)
					assert.Equal(t, "43", string(f.Subfield[0].Text))
					assert.Equal(t, "s", f.Subfield[1].Code)
					assert.Equal(t, "isil:sup2", string(f.Subfield[1].Text))
				}
				matched++
			}
		}
		assert.Equal(t, 2, matched)
	})

	t.Run("sr2.0 magic: not found", func(t *testing.T) {
		cqlQuery := "id%3Dnot-found"
		sruResp := getSr(t, url+"?maximumRecords=1&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 0)
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 1)

		assert.Equal(t, "xml", string(*sruResp.Records.Record[0].RecordXMLEscaping))
		assert.Equal(t, "info:srw/schema/1/marcxml-v1.1", sruResp.Records.Record[0].RecordSchema)

		var marc marcxml.Record
		err := xml.Unmarshal([]byte(sruResp.Records.Record[0].RecordData.XMLContent), &marc)
		assert.Nil(t, err)
		assert.Equal(t, "not-found", marc.Id)
		matched := 0
		for _, f := range marc.RecordType.Datafield {
			if f.Tag == "999" && f.Ind1 == "1" && f.Ind2 == "1" {
				matched++
			}
		}
		assert.Equal(t, 0, matched)
	})

	t.Run("sr2.0 magic: empty", func(t *testing.T) {
		cqlQuery := "id%3D%22%22"
		sruResp := getSr(t, url+"?maximumRecords=1&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Len(t, sruResp.Diagnostics.Diagnostic, 0)
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 1)

		assert.Equal(t, "xml", string(*sruResp.Records.Record[0].RecordXMLEscaping))
		assert.Equal(t, "info:srw/schema/1/marcxml-v1.1", sruResp.Records.Record[0].RecordSchema)

		var marc marcxml.Record
		err := xml.Unmarshal([]byte(sruResp.Records.Record[0].RecordData.XMLContent), &marc)
		assert.Nil(t, err)
		assert.Equal(t, "", marc.Id)
		matched := 0
		for _, f := range marc.RecordType.Datafield {
			if f.Tag == "999" && f.Ind1 == "1" && f.Ind2 == "1" {
				matched++
			}
		}
		assert.Equal(t, 0, matched)
	})

	t.Run("sr2.0 magic: error", func(t *testing.T) {
		cqlQuery := "id%3Derror"
		sruResp := getSr(t, url+"?version=2.0&maximumRecords=1&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 1, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, uint64(0), sruResp.NumberOfRecords)
		assert.Equal(t, "info:srw/diagnostic/1/2", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "System temporarily unavailable", sruResp.Diagnostics.Diagnostic[0].Message)
		assert.Equal(t, "error", sruResp.Diagnostics.Diagnostic[0].Details)
	})

	t.Run("sr2.0 magic: return-foo", func(t *testing.T) {
		cqlQuery := "id%3Dreturn-foo"
		sruResp := getSr(t, url+"?version=2.0&maximumRecords=1&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 1)
		assert.Equal(t, "xml", string(*sruResp.Records.Record[0].RecordXMLEscaping))
		assert.Equal(t, "info:srw/schema/1/marcxml-v1.1", sruResp.Records.Record[0].RecordSchema)

		var marc marcxml.Record
		err := xml.Unmarshal([]byte(sruResp.Records.Record[0].RecordData.XMLContent), &marc)
		assert.Nil(t, err)

		matched := false
		for _, f := range marc.RecordType.Datafield {
			if f.Tag == "999" && f.Ind1 == "1" && f.Ind2 == "1" {
				matched = true
				assert.Len(t, f.Subfield, 2)
				assert.Equal(t, "l", f.Subfield[0].Code)
				assert.Equal(t, "foo", string(f.Subfield[0].Text))
				assert.Equal(t, "s", f.Subfield[1].Code)
				assert.Equal(t, "foo", string(f.Subfield[1].Text))
			}
		}
		assert.True(t, matched)
	})

	t.Run("sr2.0 magic: record-error", func(t *testing.T) {
		cqlQuery := "id%3Drecord-error"
		sruResp := getSr(t, url+"?version=2.0&maximumRecords=1&query="+cqlQuery)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, uint64(1), sruResp.NumberOfRecords)
		assert.Len(t, sruResp.Records.Record, 1)
		assert.Equal(t, "xml", string(*sruResp.Records.Record[0].RecordXMLEscaping))
		assert.Equal(t, "info:srw/schema/1/diagnostics-v1.1", sruResp.Records.Record[0].RecordSchema)
		assert.Contains(t, string(sruResp.Records.Record[0].RecordData.XMLContent), "mock record error")
	})

	t.Run("sr unsupported index", func(t *testing.T) {
		sruResp := getSr(t, url+"?version=2.0&query=id")
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 1, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, "info:srw/diagnostic/1/16", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Unsupported index", sruResp.Diagnostics.Diagnostic[0].Message)
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
