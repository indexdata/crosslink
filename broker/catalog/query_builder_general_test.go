package catalog

import (
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	qb, err = NewQueryBuilderGen(&dirapi.QueryConfig{})
	assert.NoError(t, err)
	assert.NotNil(t, qb)
	gg = (qb).(*QueryBuilderGen)
	assert.Equal(t, "@attr 1=12 {term}", *gg.config.Identifier)
	assert.Equal(t, "@attr 1=7 {term}", *gg.config.Isbn)
	assert.Equal(t, "@attr 1=8 {term}", *gg.config.Issn)
	assert.Equal(t, "@attr 1=4 {term}", *gg.config.Title)

	// Test with CQL type and no mappings (should use default CQL mappings)
	cqlType := dirapi.QueryConfigTypeCql
	qb, err = NewQueryBuilderGen(&dirapi.QueryConfig{Type: &cqlType})
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
	qb, err = NewQueryBuilderGen(&dirapi.QueryConfig{
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
	assert.ErrorIs(t, err, ErrMissingLookupParameters)

	// Test with unsupported type
	unsupportedType := dirapi.QueryConfigType("unsupported")
	qb, err = NewQueryBuilderGen(&dirapi.QueryConfig{Type: &unsupportedType})
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

func TestQueryBuilderYear(t *testing.T) {
	for _, queryType := range []dirapi.QueryConfigType{dirapi.QueryConfigTypeCql, dirapi.QueryConfigTypePqf} {
		t.Run(string(queryType), func(t *testing.T) {
			template := "dc.date = {term}"
			if queryType == dirapi.QueryConfigTypePqf {
				template = "@attr 1=30 {term}"
			}
			qb, err := NewQueryBuilderGen(&dirapi.QueryConfig{Type: &queryType, Year: &template})
			require.NoError(t, err)
			params := LookupParams{Identifier: "id", Isbn: "isbn", Issn: "issn", Title: "title", Year: "2021"}
			cql, pqf, err := qb.Build(params)
			require.NoError(t, err)
			if queryType == dirapi.QueryConfigTypeCql {
				assert.Empty(t, pqf)
				assert.Equal(t, []string{
					`rec.id = id and dc.date = 2021`,
					`isbn = isbn and dc.date = 2021`,
					`issn = issn and dc.date = 2021`,
					`title = title and dc.date = 2021`,
				}, cql)
			} else {
				assert.Empty(t, cql)
				assert.Equal(t, []string{
					`@and @attr 1=12 "id" @attr 1=30 "2021"`,
					`@and @attr 1=7 "isbn" @attr 1=30 "2021"`,
					`@and @attr 1=8 "issn" @attr 1=30 "2021"`,
					`@and @attr 1=4 "title" @attr 1=30 "2021"`,
				}, pqf)
			}
			for _, year := range []string{"2021-05-01", "2012/13", "202", "20211", " 2021", "2021 ", "abcd", "２０２１", "20\"1"} {
				params.Year = year
				cql, pqf, err := qb.Build(params)
				assert.ErrorContains(t, err, "YYYY", year)
				assert.Nil(t, cql)
				assert.Nil(t, pqf)
			}
			_, _, err = qb.Build(LookupParams{Year: "2021"})
			assert.ErrorIs(t, err, ErrMissingLookupParameters)

			params.Year = ""
			withoutYear, err := NewQueryBuilderGen(&dirapi.QueryConfig{Type: &queryType})
			require.NoError(t, err)
			wantCql, wantPqf, err := withoutYear.Build(params)
			require.NoError(t, err)
			cql, pqf, err = qb.Build(params)
			require.NoError(t, err)
			assert.Equal(t, wantCql, cql)
			assert.Equal(t, wantPqf, pqf)
			for _, yearConfig := range []*string{nil, new(string)} {
				qb, err := NewQueryBuilderGen(&dirapi.QueryConfig{Type: &queryType, Year: yearConfig})
				require.NoError(t, err)
				for _, year := range []string{"2021", "invalid date"} {
					params.Year = year
					cql, pqf, err = qb.Build(params)
					require.NoError(t, err)
					assert.Equal(t, wantCql, cql)
					assert.Equal(t, wantPqf, pqf)
				}
			}
		})
	}
}

func TestQueryBuilderYearCompoundTemplates(t *testing.T) {
	cqlType := dirapi.QueryConfigTypeCql
	qb, err := NewQueryBuilderGen(&dirapi.QueryConfig{
		Type:       &cqlType,
		Identifier: NewString("rec.id = {term} or other.id = {term}"),
		Year:       NewString("dc.date = {term} or local.year = {term}"),
	})
	require.NoError(t, err)
	cql, _, err := qb.Build(LookupParams{Identifier: "123", Year: "2021"})
	require.NoError(t, err)
	assert.Equal(t, []string{`rec.id = 123 or other.id = 123 and (dc.date = 2021 or local.year = 2021)`}, cql)

	// Omitted type selects PQF and must still apply the configured year.
	qb, err = NewQueryBuilderGen(&dirapi.QueryConfig{
		Identifier: NewString("@or @attr 1=12 {term} @attr 1=4 {term}"),
		Year:       NewString("@attr 1=30 {term}"),
	})
	require.NoError(t, err)
	_, pqf, err := qb.Build(LookupParams{Identifier: "123", Year: "2021"})
	require.NoError(t, err)
	assert.Equal(t, []string{`@and @or @attr 1=12 "123" @attr 1=4 "123" @attr 1=30 "2021"`}, pqf)
}

func TestQueryBuilderYearInvalidCql(t *testing.T) {
	cqlType := dirapi.QueryConfigTypeCql
	for _, config := range []dirapi.QueryConfig{
		{Type: &cqlType, Identifier: NewString("rec.id = {term} and"), Year: NewString("dc.date = {term}")},
		{Type: &cqlType, Year: NewString("dc.date =")},
	} {
		qb, err := NewQueryBuilderGen(&config)
		require.NoError(t, err)
		_, _, err = qb.Build(LookupParams{Identifier: "record", Year: "2021"})
		assert.ErrorContains(t, err, "combining identifier lookup with year")
	}
}
