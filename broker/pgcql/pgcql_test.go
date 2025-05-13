package pgcql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/indexdata/cql-go/cql"
	"github.com/stretchr/testify/assert"
)

func TestBadSearchClause(t *testing.T) {
	def := &PgDefinition{}

	assert.Nil(t, def.GetFieldType("foo"))

	q := cql.Query{}
	_, err := def.Parse(q, 1)
	assert.Error(t, err, "Expected error for empty query")
	assert.Equal(t, "unsupported clause type", err.Error())
}

func TestParsing(t *testing.T) {
	def := &PgDefinition{}
	title := &FieldString{}
	title.WithColumn("Title")
	assert.Equal(t, title.GetColumn(), "Title", "GetColumn() should return the column name")

	author := &FieldString{}
	author.WithColumn("Author")

	serverChoice := &FieldString{}
	serverChoice.WithColumn("T")

	def.AddField("title", title).AddField("author", author).AddField("cql.serverChoice", serverChoice)

	for _, testcase := range []struct {
		query        string
		expected     string
		expectedArgs []interface{}
	}{
		{"abc", "T = $1", []interface{}{"abc"}},
		{"au=2", "-unknown field au", nil},
		{"title>2", "-unsupported operator", nil},
		{"title=2", "Title = $1", []interface{}{"2"}},
		{"title<>2", "Title != $1", []interface{}{"2"}},
		{"a or b and c", "(T = $1 OR T = $2) AND T = $3", []interface{}{"a", "b", "c"}},
		{"title = abc", "Title = $1", []interface{}{"abc"}},
		{"author = \"test\"", "Author = $1", []interface{}{"test"}},
		{"title = a AND author = b c", "Title = $1 AND Author = $2", []interface{}{"a", "b c"}},
		{"title = 'a' OR author = 'b'", "Title = $1 OR Author = $2", []interface{}{"'a'", "'b'"}},
		{"title = a NOT author = b", "Title = $1 AND NOT Author = $2", []interface{}{"a", "b"}},
		{"a prox b", "-unsupported operator prox", []interface{}{}},
		{"a sortby title", "-sorting not supported", []interface{}{}},
		{"au=2 or a", "-unknown field au", nil},
		{"a or au=2", "-unknown field au", nil},
	} {
		var parser cql.Parser
		q, err := parser.Parse(testcase.query)
		if err != nil {
			t.Errorf("%s: CQL parse error: %v", testcase.query, err)
			continue
		}
		pgQuery, err := def.Parse(q, 1)

		expectedError := strings.HasPrefix(testcase.expected, "-")

		if err != nil {
			if expectedError {
				if strings.TrimPrefix(testcase.expected, "-") != err.Error() {
					t.Errorf("%s: Expected error %s, got %s", testcase.query, strings.TrimPrefix(testcase.expected, "-"), err)
				}
			} else {
				t.Errorf("%s: Failed to parse: %v", testcase.query, err)
			}
			continue
		}
		if expectedError {
			t.Errorf("%s: Expected error, but got OK", testcase.query)
			continue
		}
		if pgQuery.GetWhereClause() != testcase.expected {
			t.Errorf("%s: Expected %s, got %s", testcase.query, testcase.expected, pgQuery.GetWhereClause())
		}
		if !reflect.DeepEqual(pgQuery.GetQueryArguments(), testcase.expectedArgs) {
			t.Errorf("%s: Expected arguments %v, got %v", testcase.query, testcase.expectedArgs, pgQuery.GetQueryArguments())
		}
		if pgQuery.GetOrderByClause() != "" {
			t.Errorf("%s: Expected empty order by clause, got %s", testcase.query, pgQuery.GetOrderByClause())
		}
		if pgQuery.GetOrderByFields() != "" {
			t.Errorf("%s: Expected empty order by fields, got %s", testcase.query, pgQuery.GetOrderByFields())
		}
	}
}
