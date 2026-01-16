package dirmock

import (
	"compress/gzip"
	"os"
	"strings"
	"testing"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

func TestMatchQueries(t *testing.T) {
	match, err := matchQuery(nil, nil)
	assert.Nil(t, err)
	assert.True(t, match)

	match, err = matchClause(nil, nil)
	assert.Nil(t, err)
	assert.False(t, match)

	for _, testcase := range []struct {
		query   string
		symbols string
		match   bool
		error   string
	}{
		{"a:a", "a:a a:b a:c", false, "cql.serverChoice"},
		{"symbol > a:a", "a:a a:b a:c", false, ""},
		{"symbol = a:a", "a:a", true, ""},
		{"symbol = a:a", "a:b", false, ""},
		{"symbol = a:a", "a:a a:b", false, ""},
		{"symbol = a:a a:b", "a:a a:b", true, ""},
		{"symbol = a:b a:a", "a:a a:b", false, ""},
		{"symbol = a:b a:a", "a:a a:b a:c", false, ""},
		{"symbol any a:a", "a:a", true, ""},
		{"symbol any a:a", "a:b", false, ""},
		{"symbol any a:a", "a:a a:b", true, ""},
		{"symbol any a:a a:b", "a:a a:b", true, ""},
		{"symbol any a:b a:a", "a:a a:b", true, ""},
		{"symbol any a:b a:a", "a:a a:b a:c", true, ""},
		{"symbol all a:a", "a:a", true, ""},
		{"symbol all a:a", "a:b", false, ""},
		{"symbol all a:a", "a:a a:b", true, ""},
		{"symbol all a:a a:b", "a:a a:b", true, ""},
		{"symbol all a:b a:a", "a:a a:b", true, ""},
		{"symbol all a:b a:a", "a:a a:b a:c", true, ""},
		{"symbol all a:b or symbol all d", "a:a a:b a:c", true, ""},
		{"symbol all e or symbol all d", "a:a a:b a:c", false, ""},
		{"symbol all e or d", "a:a a:b a:c", false, "cql.serverChoice"},
		{"e or symbol all d", "a:a a:b a:c", false, "cql.serverChoice"},
		{"symbol all a:b and symbol all d", "a:a a:b a:c", false, ""},
		{"symbol all e and symbol all d", "a:a a:b a:c", false, ""},
		{"symbol all a:a and symbol all a:c", "a:a a:b a:c", true, ""},

		{"symbol all a:b not symbol all d", "a:a a:b a:c", true, ""},
		{"symbol all e not symbol all d", "a:a a:b a:c", false, ""},
		{"symbol all a:a not symbol all a:c", "a:a a:b a:c", false, ""},
		{"symbol all a:a not symbol all a:c", "a:a a:b a:c", false, ""},
		{"symbol all a:a prox symbol all a:c", "a:a a:b a:c", false, "unsupported operator"},
	} {
		t.Run(testcase.query, func(t *testing.T) {
			var p cql.Parser
			query, err := p.Parse(testcase.query)
			if err != nil {
				t.Fatalf("failed to parse query: %v", err)
			}
			var symbols []directory.Symbol
			for _, symbol := range strings.Split(testcase.symbols, " ") {
				split := strings.Split(symbol, ":")
				symbols = append(symbols, directory.Symbol{Authority: split[0], Symbol: split[1]})
			}
			match, err := matchQuery(&query, &symbols)
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
