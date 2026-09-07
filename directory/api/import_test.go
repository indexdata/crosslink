package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/import/model"
	importservice "github.com/indexdata/crosslink/directory/import/service"
	apiValidator "github.com/oapi-codegen/nethttp-middleware"
	"github.com/stretchr/testify/require"
)

type recordingAggregateImporter struct {
	policy model.ConflictPolicy
	result model.ImportResult
	err    error
	calls  int
}

func (i *recordingAggregateImporter) Import(_ context.Context, policy model.ConflictPolicy, input io.Reader) (model.ImportResult, error) {
	i.calls++
	i.policy = policy
	_, _ = io.Copy(io.Discard, input)
	return i.result, i.err
}

func TestPostImportDefaultsPolicyAndMapsResult(t *testing.T) {
	importer := &recordingAggregateImporter{result: model.ImportResult{
		Entries: model.ImportSectionResult{Imported: 1},
		Errors:  []model.ImportItemError{},
	}}
	impl := NewApiImpl(nil, nil, importer)

	response, err := impl.PostImport(consortialAdminContext(t), PostImportRequestObject{Body: strings.NewReader("record")})

	require.NoError(t, err)
	require.IsType(t, PostImport200JSONResponse{}, response)
	require.Equal(t, model.ConflictPolicyFail, importer.policy)
	mapped := ImportResult(response.(PostImport200JSONResponse))
	require.Equal(t, int32(1), mapped.Entries.Imported)
	require.Empty(t, mapped.Errors)
}

func TestPostImportRejectsUnauthorizedCallerBeforeReadingBody(t *testing.T) {
	importer := &recordingAggregateImporter{}
	impl := NewApiImpl(nil, nil, importer)

	response, err := impl.PostImport(context.Background(), PostImportRequestObject{Body: strings.NewReader("record")})

	require.NoError(t, err)
	require.IsType(t, PostImport401TextResponse(""), response)
	require.Zero(t, importer.calls)
}

func TestPostImportValidatesPolicyAndBody(t *testing.T) {
	importer := &recordingAggregateImporter{}
	impl := NewApiImpl(nil, nil, importer)
	unknown := ConflictPolicy("unknown")

	response, err := impl.PostImport(consortialAdminContext(t), PostImportRequestObject{Params: PostImportParams{ConflictPolicy: &unknown}, Body: strings.NewReader("record")})
	require.NoError(t, err)
	require.IsType(t, PostImport400TextResponse(""), response)
	require.Zero(t, importer.calls)

	response, err = impl.PostImport(consortialAdminContext(t), PostImportRequestObject{Body: nil})
	require.NoError(t, err)
	require.IsType(t, PostImport400TextResponse(""), response)
	require.Zero(t, importer.calls)
}

func TestPostImportMapsRecordAndBodyLimitsTo413(t *testing.T) {
	for name, importErr := range map[string]error{
		"record": importservice.ErrRecordTooLarge,
		"body":   &http.MaxBytesError{Limit: 128},
	} {
		t.Run(name, func(t *testing.T) {
			importer := &recordingAggregateImporter{err: importErr}
			impl := NewApiImpl(nil, nil, importer)
			response, err := impl.PostImport(consortialAdminContext(t), PostImportRequestObject{Body: strings.NewReader("record")})
			require.NoError(t, err)
			require.IsType(t, PostImport413TextResponse(""), response)
		})
	}
}

func TestPostImportMapsFatalReaderErrorTo500(t *testing.T) {
	importer := &recordingAggregateImporter{err: errors.New("read failed")}
	impl := NewApiImpl(nil, nil, importer)
	response, err := impl.PostImport(consortialAdminContext(t), PostImportRequestObject{Body: strings.NewReader("record")})
	require.NoError(t, err)
	require.IsType(t, PostImport500TextResponse(""), response)
}

func TestPostImportHTTPValidatesBodyAndContentType(t *testing.T) {
	importer := &recordingAggregateImporter{result: model.ImportResult{Errors: []model.ImportItemError{}}}
	handler := importHTTPHandler(t, importer)

	missingBody := httptest.NewRequest(http.MethodPost, "/directory/import", http.NoBody)
	missingBody.Header.Set("Content-Type", "application/x-ndjson")
	missingBody.Header.Set(auth.FolioPermissionsHeader, `["directory.consortium.all"]`)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingBody)
	require.Equal(t, http.StatusBadRequest, missingResponse.Code)

	wrongType := httptest.NewRequest(http.MethodPost, "/directory/import", strings.NewReader("record"))
	wrongType.Header.Set("Content-Type", "application/json")
	wrongType.Header.Set(auth.FolioPermissionsHeader, `["directory.consortium.all"]`)
	wrongResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongResponse, wrongType)
	require.Equal(t, http.StatusBadRequest, wrongResponse.Code)
	require.Zero(t, importer.calls)
}

func importHTTPHandler(t *testing.T, importer AggregateImporter) http.Handler {
	t.Helper()
	spec, err := GetSpec()
	require.NoError(t, err)
	strict := NewStrictHandler(&ApiImpl{importer: importer}, nil)
	routes := HandlerWithOptions(strict, StdHTTPServerOptions{BaseURL: "/directory", BaseRouter: http.NewServeMux()})
	return auth.FolioTokenAwareMiddleware(apiValidator.OapiRequestValidator(spec)(routes))
}

func consortialAdminContext(t *testing.T) context.Context {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(auth.FolioPermissionsHeader, `["directory.consortium.all"]`)
	var result context.Context
	auth.FolioTokenAwareMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		result = request.Context()
	})).ServeHTTP(httptest.NewRecorder(), request)
	require.NotNil(t, result)
	return result
}
