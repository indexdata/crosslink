package importapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	importoapi "github.com/indexdata/crosslink/broker/import/oapi"
	"github.com/indexdata/crosslink/broker/import/service"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cache = &recordingPeerCache{peers: []ill_db.Peer{{ID: "peer-requester"}, {ID: "peer-supplier"}}}

func fixedClock() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) }

func TestPostImportDefaultsConflictPolicyToFail(t *testing.T) {
	repo := &recordingImportRepo{}
	handler := ApiHandler{importer: service.NewImporter(repo, cache, nil, nil, fixedClock)}
	recorder := newResponseRecorder()
	handler.PostImport(recorder, ndjsonRequest(`{"type":"template","owner":"ISIL:OWNER","data":`+string(validTemplateData())+`}`+"\n"), importoapi.PostImportParams{})

	assert.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, recorder.writeDeadlines, 2)
	assert.True(t, recorder.writeDeadlines[0].IsZero())
	assert.WithinDuration(t, time.Now().Add(importWriteTimeout), recorder.writeDeadlines[1], time.Second)
	assert.Equal(t, importdb.ConflictPolicyFail, repo.templatePolicy)
	assert.JSONEq(t, `{"patronRequests":{"imported":0,"failed":0,"skipped":0},"batchActions":{"imported":0,"failed":0,"skipped":0},"templates":{"imported":1,"failed":0,"skipped":0},"errors":[],"errorsOmitted":0}`, recorder.Body.String())
}

func TestPostImportDeadlinesFollowNetworkIO(t *testing.T) {
	recorder := newResponseRecorder()
	line := `{"type":"template","owner":"ISIL:OWNER","data":` + string(validTemplateData()) + "}\n"
	// Split a record across reads, and process it before reading the next one.
	input := io.MultiReader(strings.NewReader(line[:20]), strings.NewReader(line[20:]), strings.NewReader(line))
	reads := 0
	processed := 0
	nextReadDeadline := time.Now().Add(importReadTimeout)
	responseDeadline := time.Time{}
	req := ndjsonRequest("")
	req.Body = io.NopCloser(importReadFunc(func(p []byte) (int, error) {
		reads++
		require.Len(t, recorder.readDeadlines, 2*reads-1)
		deadline := recorder.readDeadlines[len(recorder.readDeadlines)-1]
		assert.False(t, deadline.Before(nextReadDeadline))
		assert.WithinDuration(t, time.Now().Add(importReadTimeout), deadline, time.Second)
		require.Len(t, recorder.writeDeadlines, 1)
		assert.True(t, recorder.writeDeadlines[0].IsZero())
		n, err := input.Read(p)
		responseDeadline = time.Now().Add(importWriteTimeout)
		return n, err
	}))
	repo := &recordingImportRepo{onImportTemplate: func() {
		processed++
		assert.True(t, recorder.readDeadlines[len(recorder.readDeadlines)-1].IsZero())
		require.Len(t, recorder.writeDeadlines, 1)
		assert.True(t, recorder.writeDeadlines[0].IsZero())
		nextReadDeadline = time.Now().Add(importReadTimeout)
	}}
	recorder.beforeWrite = func() {
		require.Len(t, recorder.writeDeadlines, 2)
		assert.False(t, recorder.writeDeadlines[1].Before(responseDeadline))
		assert.False(t, recorder.readDeadlines[len(recorder.readDeadlines)-1].IsZero())
	}
	handler := ApiHandler{importer: service.NewImporter(repo, cache, nil, nil, fixedClock)}

	handler.PostImport(recorder, req, importoapi.PostImportParams{})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 2, processed)
	assert.Greater(t, reads, processed)
	assert.Len(t, recorder.readDeadlines, 2*reads+1)
}

func TestPostImportRearmsWriteDeadlineOnReadError(t *testing.T) {
	for _, readErr := range []error{os.ErrDeadlineExceeded, errors.New("upload failed")} {
		t.Run(readErr.Error(), func(t *testing.T) {
			recorder := newResponseRecorder()
			req := ndjsonRequest("")
			var responseDeadline time.Time
			req.Body = io.NopCloser(importReadFunc(func([]byte) (int, error) {
				responseDeadline = time.Now().Add(importWriteTimeout)
				return 0, readErr
			}))
			recorder.beforeWrite = func() {
				require.Len(t, recorder.writeDeadlines, 2)
				assert.True(t, recorder.writeDeadlines[0].IsZero())
				assert.False(t, recorder.writeDeadlines[1].Before(responseDeadline))
			}
			repo := &recordingImportRepo{}
			handler := ApiHandler{importer: service.NewImporter(repo, cache, nil, nil, fixedClock)}

			handler.PostImport(recorder, req, importoapi.PostImportParams{})

			assert.Equal(t, http.StatusInternalServerError, recorder.Code)
			assert.Zero(t, repo.templateCalls)
			assert.Contains(t, recorder.Body.String(), "failed to read import request")
		})
	}
}

func TestImportReaderPropagatesDeadlineErrors(t *testing.T) {
	deadlineErr := errors.New("cannot set read deadline")
	recorder := newResponseRecorder()
	recorder.readDeadlineError = deadlineErr
	reader := importReader{
		Reader: importReadFunc(func([]byte) (int, error) {
			t.Fatal("must not read without a deadline")
			return 0, nil
		}),
		controller: http.NewResponseController(recorder),
	}

	n, err := reader.Read(make([]byte, 1))

	assert.Zero(t, n)
	assert.ErrorIs(t, err, deadlineErr)
}

func TestPostImportAcceptsExplicitConflictPolicy(t *testing.T) {
	repo := &recordingImportRepo{}
	handler := ApiHandler{importer: service.NewImporter(repo, cache, nil, nil, fixedClock)}
	policy := importoapi.Update
	recorder := newResponseRecorder()
	handler.PostImport(recorder, ndjsonRequest(`{"type":"template","owner":"ISIL:OWNER","data":`+string(validTemplateData())+`}`+"\n"), importoapi.PostImportParams{ConflictPolicy: &policy})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, importdb.ConflictPolicyUpdate, repo.templatePolicy)
}

func TestPostImportRejectsUnknownConflictPolicyBeforeBodyValidation(t *testing.T) {
	handler := ApiHandler{importer: service.NewImporter(&recordingImportRepo{}, cache, nil, nil, fixedClock)}
	policy := importoapi.ConflictPolicy("unknown")
	recorder := newResponseRecorder()
	handler.PostImport(recorder, httptest.NewRequest(http.MethodPost, "/import", http.NoBody), importoapi.PostImportParams{ConflictPolicy: &policy})

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "unknown conflict policy")
}

func TestPostImportValidatesBodyAndContentType(t *testing.T) {
	handler := ApiHandler{importer: service.NewImporter(&recordingImportRepo{}, cache, nil, nil, fixedClock)}

	missingBody := newResponseRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import", http.NoBody)
	req.Header.Set("Content-Type", "application/x-ndjson")
	handler.PostImport(missingBody, req, importoapi.PostImportParams{})
	assert.Equal(t, http.StatusBadRequest, missingBody.Code)

	wrongType := newResponseRecorder()
	req = httptest.NewRequest(http.MethodPost, "/import", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	handler.PostImport(wrongType, req, importoapi.PostImportParams{})
	assert.Equal(t, http.StatusBadRequest, wrongType.Code)
}

func TestPostImportRejectsKnownOversizedBody(t *testing.T) {
	handler := ApiHandler{
		importer:           service.NewImporter(&recordingImportRepo{}, cache, nil, nil, fixedClock),
		maxImportBodyBytes: 128,
	}
	req := ndjsonRequest(strings.Repeat("x", 129))
	recorder := newResponseRecorder()

	handler.PostImport(recorder, req, importoapi.PostImportParams{})

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestPostImportRejectsChunkedOversizedBody(t *testing.T) {
	repo := &recordingImportRepo{}
	handler := ApiHandler{
		importer:           service.NewImporter(repo, cache, nil, nil, fixedClock),
		maxImportBodyBytes: 128,
	}
	req := ndjsonRequest(strings.Repeat("x", 129))
	req.ContentLength = -1
	recorder := newResponseRecorder()

	handler.PostImport(recorder, req, importoapi.PostImportParams{})

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	assert.Zero(t, repo.patronCalls)
	assert.Zero(t, repo.templateCalls)
}

func TestPostImportRejectsOversizedRecord(t *testing.T) {
	repo := &recordingImportRepo{}
	handler := ApiHandler{importer: service.NewImporter(repo, cache, nil, nil, fixedClock)}
	req := ndjsonRequest(strings.Repeat("x", (1<<20)+1) + "\n")
	recorder := newResponseRecorder()

	handler.PostImport(recorder, req, importoapi.PostImportParams{})

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	assert.Zero(t, repo.patronCalls)
	assert.Zero(t, repo.templateCalls)
}

func ndjsonRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-ndjson")
	return req
}

type responseRecorder struct {
	*httptest.ResponseRecorder
	readDeadlines     []time.Time
	writeDeadlines    []time.Time
	readDeadlineError error
	beforeWrite       func()
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (r *responseRecorder) SetReadDeadline(deadline time.Time) error {
	r.readDeadlines = append(r.readDeadlines, deadline)
	return r.readDeadlineError
}

func (r *responseRecorder) SetWriteDeadline(deadline time.Time) error {
	r.writeDeadlines = append(r.writeDeadlines, deadline)
	return nil
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.beforeWrite != nil {
		r.beforeWrite()
	}
	r.ResponseRecorder.WriteHeader(status)
}

type importReadFunc func([]byte) (int, error)

func (f importReadFunc) Read(p []byte) (int, error) { return f(p) }

type recordingImportRepo struct {
	patron           importdb.PatronRequestBundle
	patronPolicy     importdb.ConflictPolicy
	patronResult     importdb.Result
	patronErr        error
	patronCalls      int
	template         pr_db.SaveTemplateParams
	templatePolicy   importdb.ConflictPolicy
	templateResults  []importdb.Result
	templateErrors   []error
	templateCalls    int
	onImportTemplate func()
	batch            sched_db.SaveScheduledTaskParams
	batchPolicy      importdb.ConflictPolicy
	batchResult      importdb.Result
	batchErr         error
}

func (r *recordingImportRepo) WithTxFunc(_ common.ExtendedContext, fn func(importdb.ImportRepo) error) error {
	return fn(r)
}

func (r *recordingImportRepo) ImportPatronRequest(_ common.ExtendedContext, bundle importdb.PatronRequestBundle, policy importdb.ConflictPolicy) (importdb.Result, error) {
	r.patron, r.patronPolicy, r.patronCalls = bundle, policy, r.patronCalls+1
	return r.patronResult, r.patronErr
}
func (r *recordingImportRepo) ImportTemplate(_ common.ExtendedContext, params pr_db.SaveTemplateParams, policy importdb.ConflictPolicy) (importdb.Result, error) {
	if r.onImportTemplate != nil {
		r.onImportTemplate()
	}
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
	return json.RawMessage(`{"title":"Title","purpose":"email","body":"Body","contentType":"text","labels":["first"],"audience":"patron"}`)
}

type recordingPeerCache struct {
	ill_db.PgIllRepo
	symbols []string
	peers   []ill_db.Peer
	err     error
	calls   int
}

func (c *recordingPeerCache) GetCachedPeersBySymbols(_ common.ExtendedContext, symbols []string, _ adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	c.calls++
	c.symbols = append([]string(nil), symbols...)
	return c.peers, "test", c.err
}
