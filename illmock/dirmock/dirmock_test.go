package dirmock

import (
	"os"
	"strings"
	"testing"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/crosslink/illmock/directory"
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
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "unexpected end of JSON input")
}

func TestNewEnv(t *testing.T) {
	defer os.Unsetenv("MOCK_DIRECTORY_ENTRIES")
	defer os.Unsetenv("MOCK_DIRECTORY_ENTRIES_PATH")
	os.Setenv("MOCK_DIRECTORY_ENTRIES", "{")
	_, err := NewEnv()
	assert.ErrorContains(t, err, "unexpected end of JSON input")

	os.Unsetenv("MOCK_DIRECTORY_ENTRIES")
	os.Setenv("MOCK_DIRECTORY_ENTRIES_PATH", "does-not-exist.json")
	_, err = NewEnv()
	assert.ErrorContains(t, err, "no such file or directory")

	file, err := os.CreateTemp("", "test.json")
	assert.Nil(t, err)
	defer os.Remove(file.Name())
	_, err = file.WriteString("[]")
	assert.Nil(t, err)
	err = file.Close()
	assert.Nil(t, err)
	os.Setenv("MOCK_DIRECTORY_ENTRIES_PATH", file.Name())
	_, err = NewEnv()
	assert.Nil(t, err)
}
