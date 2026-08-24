package importapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	importoapi "github.com/indexdata/crosslink/broker/import/oapi"
	"github.com/indexdata/crosslink/broker/import/service"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/stretchr/testify/assert"
)

func fixedClock() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) }

func TestPostImportDefaultsConflictPolicyToFail(t *testing.T) {
	repo := &recordingImportRepo{}
	handler := ApiHandler{importer: service.NewImporter(repo, nil, nil, nil, fixedClock)}
	recorder := httptest.NewRecorder()
	handler.PostImport(recorder, ndjsonRequest(`{"type":"template","owner":"ISIL:OWNER","data":`+string(validTemplateData())+`}`+"\n"), importoapi.PostImportParams{})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, importdb.ConflictPolicyFail, repo.templatePolicy)
	assert.JSONEq(t, `{"patronRequests":{"imported":0,"failed":0,"skipped":0},"batchActions":{"imported":0,"failed":0,"skipped":0},"templates":{"imported":1,"failed":0,"skipped":0},"errors":[]}`, recorder.Body.String())
}

func TestPostImportAcceptsExplicitConflictPolicy(t *testing.T) {
	repo := &recordingImportRepo{}
	handler := ApiHandler{importer: service.NewImporter(repo, nil, nil, nil, fixedClock)}
	policy := importoapi.Update
	recorder := httptest.NewRecorder()
	handler.PostImport(recorder, ndjsonRequest(`{"type":"template","owner":"ISIL:OWNER","data":`+string(validTemplateData())+`}`+"\n"), importoapi.PostImportParams{ConflictPolicy: &policy})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, importdb.ConflictPolicyUpdate, repo.templatePolicy)
}

func TestPostImportRejectsUnknownConflictPolicyBeforeBodyValidation(t *testing.T) {
	handler := ApiHandler{importer: service.NewImporter(&recordingImportRepo{}, nil, nil, nil, fixedClock)}
	policy := importoapi.ConflictPolicy("unknown")
	recorder := httptest.NewRecorder()
	handler.PostImport(recorder, httptest.NewRequest(http.MethodPost, "/import", http.NoBody), importoapi.PostImportParams{ConflictPolicy: &policy})

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "unknown conflict policy")
}

func TestPostImportValidatesBodyAndContentType(t *testing.T) {
	handler := ApiHandler{importer: service.NewImporter(&recordingImportRepo{}, nil, nil, nil, fixedClock)}

	missingBody := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import", http.NoBody)
	req.Header.Set("Content-Type", "application/x-ndjson")
	handler.PostImport(missingBody, req, importoapi.PostImportParams{})
	assert.Equal(t, http.StatusBadRequest, missingBody.Code)

	wrongType := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/import", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	handler.PostImport(wrongType, req, importoapi.PostImportParams{})
	assert.Equal(t, http.StatusBadRequest, wrongType.Code)
}

func ndjsonRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-ndjson")
	return req
}

type recordingImportRepo struct {
	patron          importdb.PatronRequestBundle
	patronPolicy    importdb.ConflictPolicy
	patronResult    importdb.Result
	patronErr       error
	patronCalls     int
	template        pr_db.SaveTemplateParams
	templatePolicy  importdb.ConflictPolicy
	templateResults []importdb.Result
	templateErrors  []error
	templateCalls   int
	batch           sched_db.SaveScheduledTaskParams
	batchPolicy     importdb.ConflictPolicy
	batchResult     importdb.Result
	batchErr        error
}

func (r *recordingImportRepo) WithTxFunc(_ common.ExtendedContext, fn func(importdb.ImportRepo) error) error {
	return fn(r)
}

func (r *recordingImportRepo) ImportPatronRequest(_ common.ExtendedContext, bundle importdb.PatronRequestBundle, policy importdb.ConflictPolicy) (importdb.Result, error) {
	r.patron, r.patronPolicy, r.patronCalls = bundle, policy, r.patronCalls+1
	return r.patronResult, r.patronErr
}
func (r *recordingImportRepo) ImportTemplate(_ common.ExtendedContext, params pr_db.SaveTemplateParams, policy importdb.ConflictPolicy) (importdb.Result, error) {
	r.template, r.templatePolicy, r.templateCalls = params, policy, r.templateCalls+1
	index := r.templateCalls - 1
	var result importdb.Result
	if index < len(r.templateResults) {
		result = r.templateResults[index]
	} else {
		result = importdb.Result{Outcome: importdb.OutcomeImported}
	}
	if index < len(r.templateErrors) {
		return result, r.templateErrors[index]
	}
	return result, nil
}
func (r *recordingImportRepo) ImportBatchAction(_ common.ExtendedContext, params sched_db.SaveScheduledTaskParams, policy importdb.ConflictPolicy) (importdb.Result, error) {
	r.batch, r.batchPolicy = params, policy
	return r.batchResult, r.batchErr
}
func validTemplateData() json.RawMessage {
	return json.RawMessage(`{"title":"Title","purpose":"email","body":"Body","contentType":"text/plain","labels":["first"],"audience":"patron"}`)
}
