package app

import (
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
		{"a", "a b c", false, "cql.serverChoice"},
		{"symbol > a", "a b c", false, ""},
		{"symbol = a", "a", true, ""},
		{"symbol = a", "b", false, ""},
		{"symbol = a", "a b", false, ""},
		{"symbol = a b", "a b", true, ""},
		{"symbol = b a", "a b", true, ""},
		{"symbol = b a", "a b c", false, ""},
		{"symbol any a", "a", true, ""},
		{"symbol any a", "b", false, ""},
		{"symbol any a", "a b", true, ""},
		{"symbol any a b", "a b", true, ""},
		{"symbol any b a", "a b", true, ""},
		{"symbol any b a", "a b c", true, ""},
		{"symbol all a", "a", true, ""},
		{"symbol all a", "b", false, ""},
		{"symbol all a", "a b", true, ""},
		{"symbol all a b", "a b", true, ""},
		{"symbol all b a", "a b", true, ""},
		{"symbol all b a", "a b c", true, ""},
		{"symbol all b or symbol all d", "a b c", true, ""},
		{"symbol all e or symbol all d", "a b c", false, ""},
		{"symbol all e or d", "a b c", false, "cql.serverChoice"},
		{"e or symbol all d", "a b c", false, "cql.serverChoice"},
		{"symbol all b and symbol all d", "a b c", false, ""},
		{"symbol all e and symbol all d", "a b c", false, ""},
		{"symbol all a and symbol all c", "a b c", true, ""},

		{"symbol all b not symbol all d", "a b c", true, ""},
		{"symbol all e not symbol all d", "a b c", false, ""},
		{"symbol all a not symbol all c", "a b c", false, ""},
		{"symbol all a not symbol all c", "a b c", false, ""},
		{"symbol all a prox symbol all c", "a b c", false, "unsupported operator"},
	} {
		t.Run(testcase.query, func(t *testing.T) {
			var p cql.Parser
			query, err := p.Parse(testcase.query)
			if err != nil {
				t.Fatalf("failed to parse query: %v", err)
			}
			var symbols []directory.Symbol
			for _, symbol := range strings.Split(testcase.symbols, " ") {
				symbols = append(symbols, directory.Symbol{Symbol: symbol})
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
