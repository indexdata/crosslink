package dirmock

import (
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/crosslink/directory"
)

var _ directory.StrictServerInterface = (*DirectoryMock)(nil)

type DirectoryMock struct {
	entries []directory.Entry
}

//go:embed directories.json
var defaultDirectories string

func NewEnv() (*DirectoryMock, error) {
	var entries = os.Getenv("MOCK_DIRECTORY_ENTRIES")
	if entries != "" {
		return NewJson(entries)
	}
	path := os.Getenv("MOCK_DIRECTORY_ENTRIES_PATH")
	if path == "" {
		return NewJson(defaultDirectories)
	}
	var err error
	var bytes []byte
	if strings.HasSuffix(path, ".gz") || strings.HasSuffix(path, ".gzip") || strings.HasSuffix(path, ".zip") {
		var file *os.File
		file, err = os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() {
			dErr := file.Close()
			if dErr != nil {
				fmt.Printf("error closing file: %v", dErr)
			}
		}()
		var gr *gzip.Reader
		gr, err = gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer func() {
			dErr := gr.Close()
			if dErr != nil {
				fmt.Printf("error closing reader: %v", dErr)
			}
		}()
		bytes, err = io.ReadAll(gr)
	} else {
		bytes, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	return NewJson(string(bytes))
}

func NewJson(entries string) (*DirectoryMock, error) {
	mock := &DirectoryMock{}
	err := json.Unmarshal([]byte(entries), &mock.entries)
	if err != nil {
		return nil, err
	}
	return mock, nil
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

func fixUpPeerUrl(entry *directory.Entry, peerUrl *string) {
	if entry.Endpoints == nil || peerUrl == nil {
		return
	}
	for i := range *entry.Endpoints {
		(*entry.Endpoints)[i].Address = *peerUrl
	}
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
	peerUrl := request.Params.PeerUrl
	if peerUrl != nil {
		if !strings.HasPrefix(*peerUrl, "http://") && !strings.HasPrefix(*peerUrl, "https://") {
			return directory.GetEntries400TextResponse("peerUrl must start with http:// or https://"), nil
		}
	}
	var filtered []directory.Entry
	parentmap := make(map[string][]directory.Entry)
	for _, entry := range d.entries {
		if entry.Parent == nil {
			continue
		}
		id := *entry.Parent
		fixUpPeerUrl(&entry, peerUrl)
		parentmap[id] = append(parentmap[id], entry)
	}
	for _, entry := range d.entries {
		match, err := matchQuery(query, entry.Symbols)
		if err != nil {
			return directory.GetEntries400TextResponse(err.Error()), nil
		}
		if !match {
			continue
		}
		fixUpPeerUrl(&entry, peerUrl)
		filtered = append(filtered, entry)
		if entry.Id == nil {
			continue
		}
		id := entry.Id.String()
		filtered = append(filtered, parentmap[id]...)
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
