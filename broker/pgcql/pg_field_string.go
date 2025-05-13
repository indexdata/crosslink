package pgcql

import (
	"fmt"

	"github.com/indexdata/cql-go/cql"
)

type FieldString struct {
	column string
}

func (f *FieldString) GetColumn() string {
	return f.column
}

func (f *FieldString) WithColumn(column string) Field {
	f.column = column
	return f
}

func (f *FieldString) Generate(sc cql.SearchClause, queryArgumentIndex int) (string, []interface{}, error) {
	var operator string
	switch sc.Relation {
	case cql.EQ:
		operator = "="
	case cql.NE:
		operator = "!="
	default:
		return "", nil, &PgError{message: "unsupported operator"}
	}
	return f.column + " " + operator + fmt.Sprintf(" $%d", queryArgumentIndex), []interface{}{sc.Term}, nil
}
