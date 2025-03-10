package reqform

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestReqBadIllPath(t *testing.T) {
	reqForm := &ReqForm{
		Header:   "illmock ISO18626 submit form",
		FormPath: "/form",
		IllPath:  "\x7f",
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			_, err := w.Write([]byte("OK"))
			assert.Nil(t, err)
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqForm.HandleForm(w, r)
	}))
	defer server.Close()

	resp, err := http.PostForm(server.URL, url.Values{"message": {string(Example)}})
	assert.Nil(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	buf, err := io.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Contains(t, string(buf), "error creating ISO18626 request")
}

func TestReqForm(t *testing.T) {
	reqForm := &ReqForm{
		Header:   "illmock ISO18626 submit form",
		FormPath: "/form",
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			byteReq, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var illMessage iso18626.Iso18626MessageNS
			err = xml.Unmarshal(byteReq, &illMessage)
			if err != nil {
				http.Error(w, "unmarshal: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/xml")
			w.WriteHeader(http.StatusOK)
			_, err = w.Write([]byte("<response></response>"))
			assert.Nil(t, err)
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqForm.HandleForm(w, r)
	}))
	defer server.Close()

	t.Run("get", func(t *testing.T) {
		resp, err := http.Get(server.URL)
		assert.Nil(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
		assert.Contains(t, string(buf), "post a message to see the response")
	})

	t.Run("delete", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPatch, server.URL, nil)
		assert.Nil(t, err)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		client := http.DefaultClient
		resp, err := client.Do(req)
		assert.Nil(t, err)
		assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Contains(t, string(buf), "method not allowed")
	})

	t.Run("post text/plain", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, server.URL, nil)
		assert.Nil(t, err)
		client := http.DefaultClient
		resp, err := client.Do(req)
		assert.Nil(t, err)
		assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Contains(t, string(buf), "unsupported media type")
	})

	t.Run("post www-form bad form", func(t *testing.T) {
		reader := strings.NewReader("a;b=c")
		req, err := http.NewRequest(http.MethodPost, server.URL, reader)
		assert.Nil(t, err)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		client := http.DefaultClient
		resp, err := client.Do(req)
		assert.Nil(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Contains(t, string(buf), "error parsing form")
	})

	t.Run("post www-form empty body", func(t *testing.T) {
		resp, err := http.PostForm(server.URL, url.Values{"message": {""}})
		assert.Nil(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
		assert.Contains(t, string(buf), "unmarshal: EOF")
	})

	t.Run("post www-form form ok", func(t *testing.T) {
		resp, err := http.PostForm(server.URL, url.Values{"message": {string(Example)}})
		assert.Nil(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
		assert.Contains(t, string(buf), "ISO18626 message:<")
	})
}
