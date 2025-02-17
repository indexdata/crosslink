package app

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

func TestSruService(t *testing.T) {
	api := createSruApi()
	server := httptest.NewServer(api.sruHandler())
	defer server.Close()

	t.Run("bad method", func(t *testing.T) {
		resp, err := http.Post(server.URL, "text/plain", strings.NewReader("hello"))
		assert.Nil(t, err)
		assert.Equal(t, 405, resp.StatusCode)
	})

	t.Run("sr1.1", func(t *testing.T) {
		sruUrl := server.URL + "?version=1.1&query=foo"
		resp, err := http.Get(sruUrl)
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
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 1, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, "info:srw/diagnostic/1/5", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Unsupported version", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("sr1.2", func(t *testing.T) {
		sruUrl := server.URL + "?version=1.2&query=foo"
		resp, err := http.Get(sruUrl)
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var sruResp sru.SearchRetrieveResponse
		err = xml.Unmarshal(buf, &sruResp)
		assert.Nil(t, err)
		assert.NotNil(t, sruResp.Version)
		assert.Equal(t, sru.VersionDefinition1_2, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
	})

	t.Run("sr2.0", func(t *testing.T) {
		sruUrl := server.URL + "?version=2.0&query=foo"
		resp, err := http.Get(sruUrl)
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var sruResp sru.SearchRetrieveResponse
		err = xml.Unmarshal(buf, &sruResp)
		assert.Nil(t, err)
		assert.NotNil(t, sruResp.Version)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
	})

	t.Run("exp1.1", func(t *testing.T) {
		sruUrl := server.URL + "?version=1.1"
		resp, err := http.Get(sruUrl)
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Contains(t, string(buf), "<explainResponse")
		var sruResp sru.ExplainResponse
		err = xml.Unmarshal(buf, &sruResp)
		assert.Nil(t, err)
		assert.NotNil(t, sruResp.Version)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 1, len(sruResp.Diagnostics.Diagnostic))
		assert.Equal(t, "info:srw/diagnostic/1/5", sruResp.Diagnostics.Diagnostic[0].Uri)
		assert.Equal(t, "Unsupported version", sruResp.Diagnostics.Diagnostic[0].Message)
	})

	t.Run("exp2.0", func(t *testing.T) {
		sruUrl := server.URL
		resp, err := http.Get(sruUrl)
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Contains(t, string(buf), "<explainResponse")
		var sruResp sru.ExplainResponse
		err = xml.Unmarshal(buf, &sruResp)
		assert.Nil(t, err)
		assert.NotNil(t, sruResp.Version)
		assert.Equal(t, sru.VersionDefinition2_0, *sruResp.Version)
		assert.Equal(t, 0, len(sruResp.Diagnostics.Diagnostic))
	})

}
