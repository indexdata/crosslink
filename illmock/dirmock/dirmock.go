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
	"regexp"
	"sort"
	"strings"

	"github.com/indexdata/cql-go/cql"
	apiValidator "github.com/oapi-codegen/nethttp-middleware"

	directory "github.com/indexdata/crosslink/illmock/dirmock/api"
)

var _ directory.StrictServerInterface = (*DirectoryMock)(nil)

const (
	directoryBasePath = "/rsdir"
	defaultEntryLimit = 10
	maxEntryLimit     = 1000
)

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

func matchClause(clause *cql.Clause, entry directory.Entry) (bool, error) {
	if clause == nil {
		return false, nil
	}
	if clause.SearchClause != nil {
		sc := clause.SearchClause
		switch sc.Index {
		case "name":
			return matchString(sc, entry.Name, true)
		case "description":
			return matchOptionalString(sc, entry.Description, true)
		case "type":
			if entry.Type == nil {
				return false, nil
			}
			return matchString(sc, string(*entry.Type), true)
		case "parent":
			if entry.Parent == nil {
				return false, nil
			}
			return matchString(sc, entry.Parent.String(), false)
		case "symbol":
			return matchSymbol(sc, entry.Symbols)
		case "tenant":
			return matchOptionalString(sc, entry.Tenant, false)
		default:
			return false, fmt.Errorf("unsupported index %s", sc.Index)
		}
	}
	if clause.BoolClause != nil {
		bc := clause.BoolClause
		left, err := matchClause(&bc.Left, entry)
		if err != nil {
			return false, err
		}
		right, err := matchClause(&bc.Right, entry)
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

func matchOptionalString(sc *cql.SearchClause, value *string, allowMasking bool) (bool, error) {
	if value == nil {
		return false, nil
	}
	return matchString(sc, *value, allowMasking)
}

func matchString(sc *cql.SearchClause, value string, allowMasking bool) (bool, error) {
	term, pattern, err := parseCQLTerm(sc.Term, allowMasking)
	if err != nil {
		return false, err
	}
	if pattern != nil &&
		sc.Relation != cql.EQ &&
		sc.Relation != cql.EXACT &&
		sc.Relation != cql.NE {
		return false, fmt.Errorf("masking is not supported with relation %s", sc.Relation)
	}

	switch sc.Relation {
	case cql.EQ, cql.EXACT:
		if pattern != nil {
			return pattern.MatchString(value), nil
		}
		return value == term, nil
	case cql.NE:
		if pattern != nil {
			return !pattern.MatchString(value), nil
		}
		return value != term, nil
	case cql.LT:
		return value < term, nil
	case cql.LE:
		return value <= term, nil
	case cql.GT:
		return value > term, nil
	case cql.GE:
		return value >= term, nil
	default:
		return false, fmt.Errorf("unsupported relation %s", sc.Relation)
	}
}

func parseCQLTerm(term string, allowMasking bool) (string, *regexp.Regexp, error) {
	var exact strings.Builder
	var pattern strings.Builder
	pattern.WriteString("^")
	masked := false
	escaped := false

	for _, char := range term {
		if escaped {
			switch char {
			case '*', '?', '^', '"', '\\':
				exact.WriteRune(char)
				pattern.WriteString(regexp.QuoteMeta(string(char)))
			default:
				return "", nil, fmt.Errorf("a masking backslash in a CQL string must be followed by *, ?, ^, \" or \\")
			}
			escaped = false
			continue
		}

		switch char {
		case '\\':
			escaped = true
		case '^':
			return "", nil, fmt.Errorf("anchor op ^ unsupported")
		case '*':
			if !allowMasking {
				return "", nil, fmt.Errorf("masking op * unsupported")
			}
			masked = true
			pattern.WriteString(".*")
		case '?':
			if !allowMasking {
				return "", nil, fmt.Errorf("masking op ? unsupported")
			}
			masked = true
			pattern.WriteString(".")
		default:
			exact.WriteRune(char)
			pattern.WriteString(regexp.QuoteMeta(string(char)))
		}
	}
	if escaped {
		return "", nil, fmt.Errorf("a CQL string must not end with a masking backslash")
	}
	if !masked {
		return exact.String(), nil, nil
	}
	pattern.WriteString("$")
	compiled, err := regexp.Compile(pattern.String())
	if err != nil {
		return "", nil, err
	}
	return exact.String(), compiled, nil
}

func matchSymbol(sc *cql.SearchClause, symbols *[]directory.Symbol) (bool, error) {
	if symbols == nil {
		return false, nil
	}
	matches := func(term string) bool {
		term = strings.ToUpper(term)
		for _, symbol := range *symbols {
			if fullSymbol(symbol) == term || strings.ToUpper(symbol.Symbol) == term {
				return true
			}
		}
		return false
	}

	switch sc.Relation {
	case cql.ANY:
		for _, term := range strings.Fields(sc.Term) {
			if matches(term) {
				return true, nil
			}
		}
		return false, nil
	case cql.EQ, cql.EXACT, cql.Relation("=="):
		return matches(sc.Term), nil
	default:
		return false, fmt.Errorf("unsupported relation %s for symbol", sc.Relation)
	}
}

func matchQuery(query *cql.Query, entry directory.Entry) (bool, error) {
	if query == nil {
		return true, nil
	}
	return matchClause(&query.Clause, entry)
}

func fullSymbol(symbol directory.Symbol) string {
	return strings.ToUpper(symbol.Authority + ":" + symbol.Symbol)
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

	limit := int32(defaultEntryLimit)
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := int32(0)
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	if limit < 0 || limit > maxEntryLimit || offset < 0 {
		return directory.GetEntries400TextResponse("invalid pagination parameters"), nil
	}

	filtered := make([]directory.Entry, 0)
	for _, entry := range d.entries {
		match, err := matchQuery(query, entry)
		if err != nil {
			return directory.GetEntries400TextResponse(err.Error()), nil
		}
		if !match {
			continue
		}
		filtered = append(filtered, entry)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Name != filtered[j].Name {
			return filtered[i].Name < filtered[j].Name
		}
		var left, right string
		if filtered[i].Id != nil {
			left = filtered[i].Id.String()
		}
		if filtered[j].Id != nil {
			right = filtered[j].Id.String()
		}
		return left < right
	})

	total := int64(len(filtered))
	start := min(int(offset), len(filtered))
	end := min(start+int(limit), len(filtered))
	items := append([]directory.Entry(nil), filtered[start:end]...)
	if items == nil {
		items = make([]directory.Entry, 0)
	}

	return directory.GetEntries200JSONResponse{
		Items: items,
		About: directory.About{Count: total},
	}, nil
}

func (d *DirectoryMock) HandlerFromMux(mux *http.ServeMux) error {
	swagger, err := directory.GetSpec()
	if err != nil {
		return err
	}
	sint := directory.NewStrictHandler(d, nil)
	directoryMux := http.NewServeMux()
	handler := directory.HandlerWithOptions(sint, directory.StdHTTPServerOptions{
		BaseURL:    directoryBasePath,
		BaseRouter: directoryMux,
	})
	mux.Handle(directoryBasePath+"/", apiValidator.OapiRequestValidator(swagger)(handler))
	return nil
}
