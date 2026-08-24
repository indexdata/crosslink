package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	ill_db "github.com/indexdata/crosslink/broker/ill_db"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeImportItemRejectsInvalidEnvelopes(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{`{"data":{}}`, "type is required"},
		{`{"type":"template"}`, "data is required"},
		{`{"type":"template","data":[]}`, "data must be an object"},
		{`{"type":"unknown","data":{}}`, "unknown type: unknown"},
	} {
		_, err := decodeImportItem(json.RawMessage(tt.raw))
		require.EqualError(t, err, tt.want)
	}
}

func TestImportPatronRequestNormalizesCompleteBundle(t *testing.T) {
	repo := &recordingImportRepo{patronResult: importdb.Result{Outcome: importdb.OutcomeImported}}
	validator := &recordingStateValidator{terminal: true}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "peer-requester"}, {ID: "peer-supplier"}}}
	importer := newImporter(repo, cache, nil, validator, fixedClock)

	id, result, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyUpdate, "ISIL:OWNER", validPatronBundleData())

	require.NoError(t, err)
	assert.Equal(t, "pr-1", *id)
	assert.Equal(t, importdb.OutcomeImported, result.Outcome)
	assert.Equal(t, importdb.ConflictPolicyUpdate, repo.patronPolicy)
	assert.Equal(t, "pr-1", repo.patron.PatronRequest.ID)
	assert.Equal(t, pgText("ISIL:OWNER"), repo.patron.PatronRequest.Tenant)
	assert.Equal(t, pr_db.PatronRequestSide("borrowing"), repo.patron.PatronRequest.Side)
	assert.Equal(t, pr_db.PatronRequestState("SENT"), repo.patron.PatronRequest.State)
	assert.True(t, repo.patron.PatronRequest.TerminalState)
	assert.Equal(t, fixedTime("2026-08-01T10:00:00Z"), repo.patron.PatronRequest.CreatedAt.Time)
	require.Len(t, repo.patron.Items, 1)
	assert.Equal(t, "lms-1", repo.patron.Items[0].LmsRequestID.String)
	require.Len(t, repo.patron.Notifications, 1)
	assert.True(t, repo.patron.Notifications[0].AcknowledgedAt.Valid)
	require.NotNil(t, repo.patron.IllTransaction)
	assert.Equal(t, pgText("peer-requester"), repo.patron.IllTransaction.RequesterID)
	require.Len(t, repo.patron.LocatedSuppliers, 1)
	assert.Equal(t, "peer-supplier", repo.patron.LocatedSuppliers[0].SupplierID)
	assert.Equal(t, []string{"ISIL:REQ", "ISIL:SUP"}, cache.symbols)
	assert.Equal(t, "default", validator.model)
	assert.Equal(t, proapi.Loan, validator.serviceType)
}

func TestImportPatronRequestValidatesBeforeCachingPeers(t *testing.T) {
	repo := &recordingImportRepo{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "only-one"}}}
	validator := &recordingStateValidator{err: errors.New("unsupported state")}
	importer := newImporter(repo, cache, nil, validator, fixedClock)
	_, _, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", validPatronBundleData())
	require.ErrorContains(t, err, "unsupported state")
	assert.Equal(t, 1, cache.calls)
	assert.Zero(t, repo.patronCalls)
}

func TestImportPatronRequestRejectsIncompletePeerResolution(t *testing.T) {
	repo := &recordingImportRepo{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "only-one"}}}
	importer := newImporter(repo, cache, nil, &recordingStateValidator{}, fixedClock)
	_, _, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", validPatronBundleData())
	require.ErrorContains(t, err, "expected 2 peers, got 1")
	assert.Zero(t, repo.patronCalls)
}

func TestImporterAccountsForImportedSkippedAndFailed(t *testing.T) {
	repo := &recordingImportRepo{templateResults: []importdb.Result{{Outcome: importdb.OutcomeImported}, {Outcome: importdb.OutcomeSkipped, Diagnostic: "labels already exist"}}, templateErrors: []error{nil, nil, errors.New("write failed")}}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "only-one"}}}
	importer := newImporter(repo, cache, nil, nil, fixedClock)
	body := ""
	for range 3 {
		body += `{"type":"template","owner":"ISIL:OWNER","data":` + string(validTemplateData()) + `}` + "\n"
	}
	result := importer.Import(testCtx(), importdb.ConflictPolicySkip, json.NewDecoder(strings.NewReader(body)))
	assert.Equal(t, int32(1), result.Templates.Imported)
	assert.Equal(t, int32(1), result.Templates.Skipped)
	assert.Equal(t, int32(1), result.Templates.Failed)
	require.Len(t, result.Errors, 2)
	assert.Equal(t, int32(2), result.Errors[0].Line)
	assert.Equal(t, "labels already exist", result.Errors[0].Error)
	assert.Equal(t, int32(3), result.Errors[1].Line)
	assert.Equal(t, importdb.ConflictPolicySkip, repo.templatePolicy)
}

func TestImporterForwardsPolicyToBatchAction(t *testing.T) {
	repo := &recordingImportRepo{batchResult: importdb.Result{Outcome: importdb.OutcomeImported}}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "only-one"}}}
	importer := newImporter(repo, cache, nil, nil, fixedClock)
	_, _, err := importer.importBatchAction(testCtx(), importdb.ConflictPolicyUpdate, "ISIL:OWNER", validBatchActionData())
	require.NoError(t, err)
	assert.Equal(t, importdb.ConflictPolicyUpdate, repo.batchPolicy)
	assert.Equal(t, "ISIL:OWNER", repo.batch.Owner)
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

type recordingStateValidator struct {
	model       string
	serviceType proapi.StateModelServiceType
	side        pr_db.PatronRequestSide
	state       pr_db.PatronRequestState
	terminal    bool
	err         error
}

func (v *recordingStateValidator) ValidateImportState(model string, serviceType proapi.StateModelServiceType, side pr_db.PatronRequestSide, state pr_db.PatronRequestState) (bool, error) {
	v.model, v.serviceType, v.side, v.state = model, serviceType, side, state
	return v.terminal, v.err
}

type recordingPeerCache struct {
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

func testCtx() common.ExtendedContext {
	return common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
}
func fixedClock() time.Time            { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) }
func fixedTime(value string) time.Time { parsed, _ := time.Parse(time.RFC3339, value); return parsed }
func pgText(value string) pgtype.Text  { return pgtype.Text{String: value, Valid: true} }

func validPatronBundleData() json.RawMessage {
	return json.RawMessage(`{
      "patronRequest":{"id":"pr-1","createdAt":"2026-08-01T10:00:00Z","updatedAt":"2026-08-02T10:00:00Z","illRequest":{"header":{"requestingAgencyRequestId":"pr-1"},"serviceInfo":{"serviceType":"Loan"}},"state":"SENT","side":"borrowing","requesterSymbol":"ISIL:REQ","requesterRequestId":"request-1","needsAttention":false,"stateModel":"default"},
      "items":[{"id":"item-1","barcode":"barcode-1","lmsRequestId":"lms-1","createdAt":"2026-08-01T10:01:00Z"}],
      "notifications":[{"id":"note-1","fromSymbol":"ISIL:REQ","toSymbol":"ISIL:SUP","direction":"sent","kind":"note","cost":1.25,"createdAt":"2026-08-01T10:02:00Z","acknowledgedAt":"2026-08-01T10:03:00Z"}],
      "illTransaction":{"id":"ill-1","timestamp":"2026-08-01T10:00:00Z","requesterSymbol":"ISIL:REQ","requesterRequestID":"request-1","supplierSymbol":"ISIL:SUP","illTransactionData":{"bibliographicInfo":{}}},
      "locatedSuppliers":[{"id":"located-1","supplierSymbol":"ISIL:SUP","ordinal":1,"supplierStatus":"selected","localSupplier":false}]
    }`)
}
func validTemplateData() json.RawMessage {
	return json.RawMessage(`{"title":"Title","purpose":"email","body":"Body","contentType":"text/plain","labels":["first"],"audience":"patron"}`)
}
func validBatchActionData() json.RawMessage {
	return json.RawMessage(`{"actionName":"request-aging","batchQuery":"state==NEW","schedule":"FREQ=DAILY;BYHOUR=6;BYMINUTE=0","title":"Daily aging"}`)
}
