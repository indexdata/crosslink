package pr_db

import (
	"fmt"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
)

type FieldExistsString struct {
	column          string
	table           string
	alias           string
	correlation     string
	valueExpression string
	sortExpression  string
	field           *pgcql.FieldString
}

func NewFieldExistsString(table, alias, correlation, valueExpression string) *FieldExistsString {
	field := pgcql.NewFieldString().WithLikeOps()
	field.SetColumn(valueExpression)
	return &FieldExistsString{
		table:           table,
		alias:           alias,
		correlation:     correlation,
		valueExpression: valueExpression,
		field:           field,
	}
}

func (f *FieldExistsString) GetColumn() string {
	return f.column
}

func (f *FieldExistsString) SetColumn(column string) {
	f.column = column
}

func (f *FieldExistsString) Sort() string {
	return f.sortExpression
}

func (f *FieldExistsString) WithSortExpression(sortExpression string) *FieldExistsString {
	f.sortExpression = sortExpression
	return f
}

func (f *FieldExistsString) WithField(field *pgcql.FieldString) *FieldExistsString {
	f.field = field
	f.field.SetColumn(f.valueExpression)
	return f
}

func (f *FieldExistsString) Generate(sc cql.SearchClause, queryArgumentIndex int) (string, []any, error) {
	switch {
	case sc.Term == "" && isPositiveStringRelation(sc.Relation):
		return "NOT " + f.existsSql(f.nonEmptyValuePredicate()), nil, nil
	case sc.Term == "" && sc.Relation == cql.NE:
		return f.existsSql(f.nonEmptyValuePredicate()), nil, nil
	case sc.Relation == cql.NE:
		sc.Relation = cql.EQ
		predicate, args, err := f.field.Generate(sc, queryArgumentIndex)
		if err != nil {
			return "", nil, err
		}
		return "NOT " + f.existsSql(predicate), args, nil
	default:
		predicate, args, err := f.field.Generate(sc, queryArgumentIndex)
		if err != nil {
			return "", nil, err
		}
		return f.existsSql(predicate), args, nil
	}
}

func isPositiveStringRelation(relation cql.Relation) bool {
	return relation == cql.EQ || relation == cql.EXACT || relation == "=="
}

func (f *FieldExistsString) existsSql(predicate string) string {
	source := f.table
	if f.alias != "" {
		source += " " + f.alias
	}
	return fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s AND %s)", source, f.correlation, predicate)
}

func (f *FieldExistsString) nonEmptyValuePredicate() string {
	return fmt.Sprintf("COALESCE(%s, '') <> ''", f.valueExpression)
}
