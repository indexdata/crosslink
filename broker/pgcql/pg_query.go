package pgcql

import (
	"fmt"

	"github.com/indexdata/cql-go/cql"
)

type PgQuery struct {
	def                *PgDefinition
	queryArgumentIndex int
	arguments          []interface{}
	sql                string
}

func (p *PgQuery) parse(q cql.Query, queryArgumentIndex int, def *PgDefinition) error {
	p.def = def
	p.arguments = make([]interface{}, 0)
	p.queryArgumentIndex = queryArgumentIndex
	if q.SortSpec != nil {
		return &PgError{message: "sorting not supported"}
	}
	return p.parseClause(q.Clause, 0)
}

func (p *PgQuery) parseClause(sc cql.Clause, level int) error {
	if sc.SearchClause != nil {
		index := sc.SearchClause.Index
		fieldType := p.def.GetFieldType(index)
		if fieldType == nil {
			return &PgError{message: fmt.Sprintf("unknown field %s", index)}
		}
		sql, args, err := fieldType.Generate(*sc.SearchClause, p.queryArgumentIndex)
		if err != nil {
			return err
		}
		p.sql += sql
		if args != nil {
			p.queryArgumentIndex += len(args)
			p.arguments = append(p.arguments, args...)
		}
		return nil
	} else if sc.BoolClause != nil {
		if level > 0 {
			p.sql += "("
		}
		err := p.parseClause(sc.BoolClause.Left, level+1)
		if err != nil {
			return err
		}
		if sc.BoolClause.Operator == cql.AND {
			p.sql += " AND "
		} else if sc.BoolClause.Operator == cql.OR {
			p.sql += " OR "
		} else if sc.BoolClause.Operator == cql.NOT {
			p.sql += " AND NOT "
		} else {
			return &PgError{message: fmt.Sprintf("unsupported operator %s", sc.BoolClause.Operator)}
		}
		err = p.parseClause(sc.BoolClause.Right, level+1)
		if err != nil {
			return err
		}
		if level > 0 {
			p.sql += ")"
		}
		return nil
	}
	return &PgError{message: "unsupported clause type"}
}

func (p *PgQuery) GetWhereClause() string {
	return p.sql
}

func (p *PgQuery) GetQueryArguments() []interface{} {
	return p.arguments
}

func (p *PgQuery) GetOrderByClause() string {
	return ""
}

func (p *PgQuery) GetOrderByFields() string {
	return ""
}
