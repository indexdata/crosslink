package pgcql

import "github.com/indexdata/cql-go/cql"

type PgDefinition struct {
	fields map[string]Field
}

func (pg *PgDefinition) AddField(name string, field Field) Definition {
	if pg.fields == nil {
		pg.fields = make(map[string]Field)
	}
	pg.fields[name] = field
	return pg
}

func (pg *PgDefinition) GetFieldType(name string) Field {
	if field, ok := pg.fields[name]; ok {
		return field
	}
	return nil
}

func (pg *PgDefinition) Parse(q cql.Query, queryArgumentIndex int) (Query, error) {
	query := &PgQuery{}
	err := query.parse(q, queryArgumentIndex, pg)
	return query, err
}
