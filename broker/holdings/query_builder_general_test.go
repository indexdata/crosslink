package holdings

import (
	"testing"

	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

func TestNewQueryBuilderGen(t *testing.T) {
	// Test with nil config (should use default PQF mappings)
	qb, err := NewQueryBuilderGen(nil)
	assert.NoError(t, err)
	assert.NotNil(t, qb)

	gg := (qb).(*QueryBuilderGen)
	assert.Equal(t, "@attr 1=12 {term}", *gg.config.Identifier)
	assert.Equal(t, "@attr 1=7 {term}", *gg.config.Isbn)
	assert.Equal(t, "@attr 1=8 {term}", *gg.config.Issn)
	assert.Equal(t, "@attr 1=4 {term}", *gg.config.Title)

	// Test with empty config (should use default PQF mappings)
	qb, err = NewQueryBuilderGen(&directory.QueryConfig{})
	assert.NoError(t, err)
	assert.NotNil(t, qb)
	gg = (qb).(*QueryBuilderGen)
	assert.Equal(t, "@attr 1=12 {term}", *gg.config.Identifier)
	assert.Equal(t, "@attr 1=7 {term}", *gg.config.Isbn)
	assert.Equal(t, "@attr 1=8 {term}", *gg.config.Issn)
	assert.Equal(t, "@attr 1=4 {term}", *gg.config.Title)

	// Test with CQL type and no mappings (should use default CQL mappings)
	cqlType := directory.Cql
	qb, err = NewQueryBuilderGen(&directory.QueryConfig{Type: &cqlType})
	assert.NoError(t, err)
	assert.NotNil(t, qb)
	gg = (qb).(*QueryBuilderGen)
	assert.Equal(t, "rec.id = {term}", *gg.config.Identifier)
	assert.Equal(t, "isbn = {term}", *gg.config.Isbn)
	assert.Equal(t, "issn = {term}", *gg.config.Issn)
	assert.Equal(t, "title = {term}", *gg.config.Title)

	cql, pqf, err := qb.Build(LookupParams{Identifier: "12345", Title: "Test Title"})
	assert.NoError(t, err)
	assert.Len(t, pqf, 0)
	assert.Equal(t, []string{"rec.id = \"12345\"", "title = \"Test Title\""}, cql)

	empty := ""
	// Test with CQL type and one mapping
	qb, err = NewQueryBuilderGen(&directory.QueryConfig{
		Type:       &cqlType,
		Identifier: NewString("id == {term}"),
		Title:      &empty,
	})
	assert.NoError(t, err)
	assert.NotNil(t, qb)
	gg = (qb).(*QueryBuilderGen)
	assert.Equal(t, "id == {term}", *gg.config.Identifier)
	assert.Equal(t, "isbn = {term}", *gg.config.Isbn)
	assert.Equal(t, "issn = {term}", *gg.config.Issn)
	assert.Equal(t, "", *gg.config.Title)
	cql, pqf, err = qb.Build(LookupParams{Identifier: "12345", Title: "Test Title"})
	assert.NoError(t, err)
	assert.Len(t, pqf, 0)
	assert.Equal(t, []string{"id == \"12345\""}, cql)

	// Test with missing lookup parameters
	_, _, err = qb.Build(LookupParams{Title: "Test Title"})
	assert.ErrorContains(t, err, "missing lookup parameters. Provide at least one of: identifier, isbn, issn")

	// Test with unsupported type
	unsupportedType := directory.QueryConfigType("unsupported")
	qb, err = NewQueryBuilderGen(&directory.QueryConfig{Type: &unsupportedType})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported query builder type")
	assert.Nil(t, qb)
}

func TestPqfEncode(t *testing.T) {
	assert.Equal(t, "\"computer\"", pqfEncode("computer"))
	assert.Equal(t, "\"co?puter*\"", pqfEncode("co?puter*"))
	assert.Equal(t, "\"comp\\\"uter\"", pqfEncode("comp\"uter"))
	assert.Equal(t, "\"comp\\\\uter\"", pqfEncode("comp\\uter"))
	assert.Equal(t, "\"comp\\\\\\\"uter\"", pqfEncode("comp\\\"uter"))
}

func TestCqlEncode(t *testing.T) {
	assert.Equal(t, "\"computer\"", cqlEncode("computer"))
	assert.Equal(t, "\"co\\?puter\\*\"", cqlEncode("co?puter*"))
	assert.Equal(t, "\"comp\\\"uter\"", cqlEncode("comp\"uter"))
	assert.Equal(t, "\"comp\\\\uter\"", cqlEncode("comp\\uter"))
	assert.Equal(t, "\"comp\\\\\\\"uter\"", cqlEncode("comp\\\"uter"))
}
