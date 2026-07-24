package dirmock

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cql"
	directory "github.com/indexdata/crosslink/illmock/dirmock/api"
	"github.com/stretchr/testify/assert"
)

func TestGetEntriesContract(t *testing.T) {
	mock, err := NewJson(`[
		{"id":"00000000-0000-0000-0000-000000000001","name":"Alpha","type":"Institution"},
		{"id":"00000000-0000-0000-0000-000000000002","name":"Beta","type":"Institution"},
		{"id":"00000000-0000-0000-0000-000000000003","name":"Gamma","type":"Institution"}
	]`)
	assert.NoError(t, err)

	mux := http.NewServeMux()
	assert.NoError(t, mock.HandlerFromMux(mux))

	for _, testcase := range []struct {
		name       string
		path       string
		status     int
		itemCount  int
		totalCount int64
	}{
		{"zero limit", "/rsdir/entries?limit=0", http.StatusOK, 0, 3},
		{"offset beyond results", "/rsdir/entries?offset=99", http.StatusOK, 0, 3},
		{"negative limit", "/rsdir/entries?limit=-1", http.StatusBadRequest, 0, 0},
		{"limit above maximum", "/rsdir/entries?limit=1001", http.StatusBadRequest, 0, 0},
		{"negative offset", "/rsdir/entries?offset=-1", http.StatusBadRequest, 0, 0},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testcase.path, nil)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			assert.Equal(t, testcase.status, recorder.Code)
			if testcase.status != http.StatusOK {
				return
			}

			var response directory.EntriesResponse
			assert.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Len(t, response.Items, testcase.itemCount)
			assert.Equal(t, testcase.totalCount, response.About.Count)
		})
	}
}

func TestMatchQueries(t *testing.T) {
	match, err := matchQuery(nil, directory.Entry{})
	assert.Nil(t, err)
	assert.True(t, match)

	match, err = matchClause(nil, directory.Entry{})
	assert.Nil(t, err)
	assert.False(t, match)

	description := "A useful description"
	tenant := "tenant-a"
	entryType := directory.EntryTypeInstitution
	parent := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	symbols := []directory.Symbol{
		{Authority: "AUTH", Symbol: "ONE"},
		{Authority: "AUTH", Symbol: "TWO"},
	}
	entry := directory.Entry{
		Name:        "Alpha Institution",
		Description: &description,
		Tenant:      &tenant,
		Type:        &entryType,
		Parent:      &parent,
		Symbols:     &symbols,
	}

	for _, testcase := range []struct {
		query string
		match bool
		error string
	}{
		{`name = "Alpha Institution"`, true, ""},
		{`name = "Alpha*"`, true, ""},
		{`name = "Beta*"`, false, ""},
		{`description = "*useful*"`, true, ""},
		{`type = Institution`, true, ""},
		{`parent = "00000000-0000-0000-0000-000000000001"`, true, ""},
		{`tenant = tenant-a`, true, ""},
		{`tenant > tenant-a`, false, ""},
		{`symbol = AUTH:ONE`, true, ""},
		{`symbol = one`, true, ""},
		{`symbol any "AUTH:THREE AUTH:TWO"`, true, ""},
		{`symbol = AUTH:THREE`, false, ""},
		{`symbol all "AUTH:ONE AUTH:TWO"`, false, "unsupported relation all for symbol"},
		{`symbol > AUTH:ONE`, false, "unsupported relation > for symbol"},
		{`foo = value`, false, "unsupported index foo"},
		{`name = "Alpha*" and tenant = tenant-a`, true, ""},
		{`name = "Beta*" or symbol = AUTH:TWO`, true, ""},
		{`symbol = AUTH:ONE not tenant = other`, true, ""},
		{`symbol = AUTH:ONE prox tenant = tenant-a`, false, "unsupported operator prox"},
		{`name = "Alpha^"`, false, "anchor op ^ unsupported"},
	} {
		t.Run(testcase.query, func(t *testing.T) {
			var p cql.Parser
			query, err := p.Parse(testcase.query)
			if err != nil {
				t.Fatalf("failed to parse query: %v", err)
			}
			match, err := matchQuery(&query, entry)
			if err != nil {
				if testcase.error == "" {
					t.Fatalf("unexpected error: %v", err)
				}
				assert.Contains(t, err.Error(), testcase.error)
			} else {
				assert.Nil(t, err)
				if match != testcase.match {
					t.Fatalf("expected match %v, got %v", testcase.match, match)
				}
			}
		})
	}
}

func TestNewJson(t *testing.T) {
	_, err := NewJson("{")
	assert.Error(t, err)
	assert.ErrorContains(t, err, "unexpected end of JSON input")
}

func TestNewEnv(t *testing.T) {
	defer func() {
		dErr := os.Unsetenv("MOCK_DIRECTORY_ENTRIES")
		assert.NoError(t, dErr, "failed unset env")
	}()
	defer func() {
		dErr := os.Unsetenv("MOCK_DIRECTORY_ENTRIES_PATH")
		assert.NoError(t, dErr, "failed unset env")
	}()
	err := os.Setenv("MOCK_DIRECTORY_ENTRIES", "{")
	assert.NoError(t, err, "failed to set env")
	_, err = NewEnv()
	assert.ErrorContains(t, err, "unexpected end of JSON input")

	err = os.Unsetenv("MOCK_DIRECTORY_ENTRIES")
	assert.NoError(t, err, "failed to set env")
	err = os.Setenv("MOCK_DIRECTORY_ENTRIES_PATH", "does-not-exist.json")
	assert.NoError(t, err, "failed to set env")
	_, err = NewEnv()
	assert.ErrorContains(t, err, "no such file or directory")

	file1, err := os.CreateTemp("", "test.json")
	assert.NoError(t, err, "failed to create temp file")
	defer func() {
		dErr := os.Remove(file1.Name())
		assert.NoError(t, dErr, "failed to remove file")
	}()
	_, err = file1.WriteString("[]")
	assert.NoError(t, err, "failed to write to temp file")
	err = file1.Close()
	assert.NoError(t, err, "failed to close temp file")
	err = os.Setenv("MOCK_DIRECTORY_ENTRIES_PATH", file1.Name())
	assert.NoError(t, err, "failed to set env")
	_, err = NewEnv()
	assert.NoError(t, err, "failed to create new env")

	file2, err := os.CreateTemp("", "test.json.*.gz")
	assert.NoError(t, err, "failed to create temp file")
	defer func() {
		dErr := os.Remove(file2.Name())
		assert.NoError(t, dErr, "failed to remove file")
	}()
	gzipWriter := gzip.NewWriter(file2)
	_, err = gzipWriter.Write([]byte("[]"))
	assert.NoError(t, err, "failed to write to gzip file")
	err = gzipWriter.Close()
	assert.NoError(t, err, "failed to close gzip writer")
	err = os.Setenv("MOCK_DIRECTORY_ENTRIES_PATH", file2.Name())
	assert.NoError(t, err, "failed to set env")
	_, err = NewEnv()
	assert.NoError(t, err, "failed to create new env")
}
