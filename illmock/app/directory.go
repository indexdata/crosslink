package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/crosslink/illmock/directory"
)

var _ directory.StrictServerInterface = (*DirectoryMock)(nil)

type DirectoryMock struct{}

func matchClause(clause *cql.Clause, symbols *[]directory.Symbol) (bool, error) {
	if symbols == nil {
		return false, nil
	}
	if clause.SearchClause != nil {
		sc := clause.SearchClause
		if sc.Index != "symbol" {
			return false, fmt.Errorf("unsupported index %s", sc.Index)
		}
		tSymbols := strings.Split(sc.Term, " ")
		switch sc.Relation {
		case cql.ANY:
			for _, t := range tSymbols {
				for _, s := range *symbols {
					if s.Symbol == t {
						return true, nil
					}
				}
			}
			return false, nil
		case cql.ALL:
			for _, t := range tSymbols {
				found := false
				for _, s := range *symbols {
					if s.Symbol == t {
						found = true
					}
				}
				if !found {
					return false, nil
				}
			}
			return true, nil
		case "=":
			if len(tSymbols) != len(*symbols) {
				return false, nil
			}
			for _, t := range tSymbols {
				found := false
				for _, s := range *symbols {
					if s.Symbol == t {
						found = true
					}
				}
				if !found {
					return false, nil
				}
			}
			return true, nil
		}
	}
	if clause.BoolClause != nil {
		bc := clause.BoolClause
		left, err := matchClause(&bc.Left, symbols)
		if err != nil {
			return false, err
		}
		right, err := matchClause(&bc.Right, symbols)
		if err != nil {
			return false, err
		}
		switch bc.Operator {
		case cql.AND:
			return left && right, nil
		case cql.OR:
			return left || right, nil
		case cql.NOT:
			return left && !right, nil
		default:
			return false, fmt.Errorf("unsupported operator %s", bc.Operator)
		}
	}
	return false, nil
}

func matchQuery(query *cql.Query, symbols *[]directory.Symbol) (bool, error) {
	if query == nil {
		return true, nil
	}
	return matchClause(&query.Clause, symbols)
}

func (d *DirectoryMock) GetEntries(ctx context.Context, request directory.GetEntriesRequestObject) (directory.GetEntriesResponseObject, error) {
	log.Info("GetEntries ", "cql", request.Params.Cql, "limit", request.Params.Limit, "offset", request.Params.Offset)

	var query *cql.Query
	if request.Params.Cql != nil {
		var p cql.Parser
		tmp, err := p.Parse(*request.Params.Cql)
		if err != nil {
			return directory.GetEntries400TextResponse(err.Error()), nil
		}
		query = &tmp
	}

	id := uuid.New()
	symbols := []directory.Symbol{
		{
			Symbol: "sym1",
		},
		{
			Symbol: "sym2",
		},
	}
	entry := directory.Entry{
		Name:    "diku",
		Id:      &id,
		Symbols: &symbols,
	}

	var entries directory.GetEntries200JSONResponse
	match, err := matchQuery(query, entry.Symbols)
	if err != nil {
		return directory.GetEntries400TextResponse(err.Error()), nil
	}
	if match {
		entries = append(entries, entry)
	}
	return entries, nil
}

func NewDirectoryMock() *DirectoryMock {
	return &DirectoryMock{}
}
