package dirmock

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/indexdata/go-utils/utils"
	"net/http"
	"strings"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/crosslink/illmock/directory"
)

var _ directory.StrictServerInterface = (*DirectoryMock)(nil)

type DirectoryMock struct{}

//go:embed directories.json
var defaultDirectories string

var DIRECTORY_ENTRIES = utils.GetEnv("MOCK_DIRECTORY_ENTRIES", defaultDirectories)

func New() *DirectoryMock {
	return &DirectoryMock{}
}

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
					if fullSymbol(s) == t {
						return true, nil
					}
				}
			}
			return false, nil
		case cql.ALL:
			for _, t := range tSymbols {
				found := false
				for _, s := range *symbols {
					if fullSymbol(s) == t {
						found = true
					}
				}
				if !found {
					return false, nil
				}
			}
			return true, nil
		case "=":
			// all match match in order
			if len(tSymbols) != len(*symbols) {
				return false, nil
			}
			for i, t := range tSymbols {
				if t != fullSymbol((*symbols)[i]) {
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

func fullSymbol(symbol directory.Symbol) string {
	return symbol.Authority + ":" + symbol.Symbol
}

func (d *DirectoryMock) GetEntries(ctx context.Context, request directory.GetEntriesRequestObject) (directory.GetEntriesResponseObject, error) {
	var query *cql.Query
	if request.Params.Cql != nil {
		var p cql.Parser
		tmp, err := p.Parse(*request.Params.Cql)
		if err != nil {
			return directory.GetEntries400TextResponse(err.Error()), nil
		}
		query = &tmp
	}

	var entries []directory.Entry
	var filtered []directory.Entry
	err := json.Unmarshal([]byte(DIRECTORY_ENTRIES), &entries)
	if err != nil {
		return directory.GetEntries500TextResponse(err.Error()), nil
	}
	for _, entry := range entries {
		match, err := matchQuery(query, entry.Symbols)
		if err != nil {
			return directory.GetEntries400TextResponse(err.Error()), nil
		}
		if match {
			filtered = append(filtered, entry)
		}
	}
	var response directory.GetEntries200JSONResponse
	response.Items = filtered
	total := len(filtered)
	response.ResultInfo = &directory.ResultInfo{
		TotalRecords: &total,
	}
	return response, nil
}

func (d *DirectoryMock) HandlerFromMux(mux *http.ServeMux) {
	sint := directory.NewStrictHandler(d, nil)
	directory.HandlerFromMux(sint, mux)
}
