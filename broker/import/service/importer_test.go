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
		{`{"type":"template","owner":"ISIL:SYM","data":{},"unexpected":true}`, `json: unknown field "unexpected"`},
		{`{"type":"template","owner":"ISIL:SYM","data":{}} {}`, "multiple JSON values"},
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
	assert.Equal(t, pgText("lms-item-1"), repo.patron.Items[0].LmsItemID)
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

func TestImportPatronRequestRejectsMissingNeedsAttention(t *testing.T) {
	repo := &recordingImportRepo{patronResult: importdb.Result{Outcome: importdb.OutcomeImported}}
	validator := &recordingStateValidator{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "peer-requester"}, {ID: "peer-supplier"}}}
	importer := newImporter(repo, cache, nil, validator, fixedClock)
	data := mutatePatronBundleData(t, func(bundle map[string]any) {
		delete(bundle["patronRequest"].(map[string]any), "needsAttention")
	})

	_, _, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", data)

	require.ErrorContains(t, err, "needsAttention")
	assert.Zero(t, repo.patronCalls)
}

func TestImportPatronRequestRejectsUnknownProperty(t *testing.T) {
	repo := &recordingImportRepo{patronResult: importdb.Result{Outcome: importdb.OutcomeImported}}
	validator := &recordingStateValidator{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "peer-requester"}, {ID: "peer-supplier"}}}
	importer := newImporter(repo, cache, nil, validator, fixedClock)
	data := mutatePatronBundleData(t, func(bundle map[string]any) {
		bundle["unexpected"] = true
	})

	_, _, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", data)

	require.ErrorContains(t, err, "unexpected")
	assert.Zero(t, repo.patronCalls)
}

func TestImporterPreservesPatronRequestIdentifierOnSchemaError(t *testing.T) {
	repo := &recordingImportRepo{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
	importer := newImporter(repo, cache, nil, &recordingStateValidator{}, fixedClock)
	data := mutatePatronBundleData(t, func(bundle map[string]any) {
		bundle["patronRequest"].(map[string]any)["side"] = "other"
	})
	record := `{"type":"patronRequest","owner":"ISIL:OWNER","data":` + string(data) + `}`

	result, err := importer.Import(testCtx(), importdb.ConflictPolicyFail, strings.NewReader(record))

	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	require.NotNil(t, result.Errors[0].Identifier)
	assert.Equal(t, "pr-1", *result.Errors[0].Identifier)
}

func TestImportPatronRequestAllowsEmptyCollections(t *testing.T) {
	repo := &recordingImportRepo{patronResult: importdb.Result{Outcome: importdb.OutcomeImported}}
	validator := &recordingStateValidator{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
	importer := newImporter(repo, cache, nil, validator, fixedClock)
	data := mutatePatronBundleData(t, func(bundle map[string]any) {
		bundle["items"] = []any{}
		bundle["notifications"] = []any{}
		bundle["locatedSuppliers"] = []any{}
		delete(bundle, "illTransaction")
	})

	_, _, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", data)

	require.NoError(t, err)
	assert.Empty(t, repo.patron.Items)
	assert.Empty(t, repo.patron.Notifications)
	assert.Empty(t, repo.patron.LocatedSuppliers)
	assert.Equal(t, 1, cache.calls)
}

func TestImportPatronRequestRejectsIllTransactionForLendingRequest(t *testing.T) {
	repo := &recordingImportRepo{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
	importer := newImporter(repo, cache, nil, &recordingStateValidator{}, fixedClock)
	data := mutatePatronBundleData(t, func(bundle map[string]any) {
		bundle["patronRequest"].(map[string]any)["side"] = "lending"
	})

	_, _, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", data)

	require.ErrorContains(t, err, "illTransaction is only allowed for borrowing patron requests")
	assert.Zero(t, repo.patronCalls)
}

func TestImportPatronRequestRejectsMissingOrBlankSupplierSymbolForLending(t *testing.T) {
	tests := []struct {
		name           string
		supplierSymbol *string
	}{
		{name: "missing"},
		{name: "empty", supplierSymbol: stringPointer("")},
		{name: "whitespace", supplierSymbol: stringPointer(" \t ")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingImportRepo{}
			cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
			importer := newImporter(repo, cache, nil, &recordingStateValidator{}, fixedClock)
			data := mutatePatronBundleData(t, func(bundle map[string]any) {
				request := bundle["patronRequest"].(map[string]any)
				request["side"] = "lending"
				if tt.supplierSymbol == nil {
					delete(request, "supplierSymbol")
				} else {
					request["supplierSymbol"] = *tt.supplierSymbol
				}
				delete(bundle, "illTransaction")
				bundle["locatedSuppliers"] = []any{}
			})

			_, _, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", data)

			require.ErrorContains(t, err, "patronRequest.supplierSymbol is required for lending requests")
			assert.Zero(t, repo.patronCalls)
		})
	}
}

func TestImportPatronRequestRejectsSchemaInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		mutate func(map[string]any)
	}{
		{
			name: "empty patron request id",
			path: "/patronRequest/id",
			mutate: func(bundle map[string]any) {
				bundle["patronRequest"].(map[string]any)["id"] = ""
			},
		},
		{
			name: "invalid side",
			path: "/patronRequest/side",
			mutate: func(bundle map[string]any) {
				bundle["patronRequest"].(map[string]any)["side"] = "other"
			},
		},
		{
			name: "empty item barcode",
			path: "/items/0/barcode",
			mutate: func(bundle map[string]any) {
				bundle["items"].([]any)[0].(map[string]any)["barcode"] = ""
			},
		},
		{
			name: "invalid notification direction",
			path: "/notifications/0/direction",
			mutate: func(bundle map[string]any) {
				bundle["notifications"].([]any)[0].(map[string]any)["direction"] = "other"
			},
		},
		{
			name: "null ILL transaction data",
			path: "/illTransaction/illTransactionData",
			mutate: func(bundle map[string]any) {
				bundle["illTransaction"].(map[string]any)["illTransactionData"] = nil
			},
		},
		{
			name: "empty located supplier id",
			path: "/locatedSuppliers/0/id",
			mutate: func(bundle map[string]any) {
				bundle["locatedSuppliers"].([]any)[0].(map[string]any)["id"] = ""
			},
		},
		{
			name: "missing items",
			path: "items",
			mutate: func(bundle map[string]any) {
				delete(bundle, "items")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingImportRepo{}
			cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
			importer := newImporter(repo, cache, nil, &recordingStateValidator{}, fixedClock)
			data := mutatePatronBundleData(t, tt.mutate)

			_, _, err := importer.importPatronRequest(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", data)

			require.ErrorContains(t, err, "validate patron request")
			require.ErrorContains(t, err, tt.path)
			assert.Zero(t, repo.patronCalls)
		})
	}
}

func TestImporterAccountsForImportedSkippedAndFailed(t *testing.T) {
	repo := &recordingImportRepo{templateResults: []importdb.Result{{Outcome: importdb.OutcomeImported}, {Outcome: importdb.OutcomeSkipped, Diagnostic: "labels already exist"}}, templateErrors: []error{nil, nil, errors.New("write failed")}}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "only-one"}}}
	importer := newImporter(repo, cache, nil, nil, fixedClock)
	body := ""
	for range 3 {
		body += `{"type":"template","owner":"ISIL:OWNER","data":` + string(validTemplateData()) + `}` + "\n"
	}
	result, err := importer.Import(testCtx(), importdb.ConflictPolicySkip, strings.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, int32(1), result.Templates.Imported)
	assert.Equal(t, int32(1), result.Templates.Skipped)
	assert.Equal(t, int32(1), result.Templates.Failed)
	require.Len(t, result.Errors, 2)
	assert.Equal(t, int32(2), result.Errors[0].Line)
	assert.Equal(t, "labels already exist", result.Errors[0].Error)
	assert.Equal(t, int32(3), result.Errors[1].Line)
	assert.Equal(t, importdb.ConflictPolicySkip, repo.templatePolicy)
}

func TestImporterCapsFailureDetailsWhilePreservingCounters(t *testing.T) {
	const recordCount = 105
	repo := &recordingImportRepo{templateErrors: make([]error, recordCount)}
	for index := range repo.templateErrors {
		repo.templateErrors[index] = errors.New("write failed")
	}
	importer := newImporter(repo, &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}, nil, nil, fixedClock)
	record := `{"type":"template","owner":"ISIL:OWNER","data":` + string(validTemplateData()) + `}` + "\n"

	result, err := importer.Import(testCtx(), importdb.ConflictPolicyFail, strings.NewReader(strings.Repeat(record, recordCount)))

	require.NoError(t, err)
	assert.Equal(t, int32(recordCount), result.Templates.Failed)
	assert.Len(t, result.Errors, 100)
	assertImportErrorsOmitted(t, result, 5)
}

func TestImporterCapsSkippedDetailsWhilePreservingCounters(t *testing.T) {
	const recordCount = 105
	repo := &recordingImportRepo{templateResults: make([]importdb.Result, recordCount)}
	for index := range repo.templateResults {
		repo.templateResults[index] = importdb.Result{Outcome: importdb.OutcomeSkipped, Diagnostic: "labels already exist"}
	}
	importer := newImporter(repo, &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}, nil, nil, fixedClock)
	record := `{"type":"template","owner":"ISIL:OWNER","data":` + string(validTemplateData()) + `}` + "\n"

	result, err := importer.Import(testCtx(), importdb.ConflictPolicySkip, strings.NewReader(strings.Repeat(record, recordCount)))

	require.NoError(t, err)
	assert.Equal(t, int32(recordCount), result.Templates.Skipped)
	assert.Len(t, result.Errors, 100)
	assertImportErrorsOmitted(t, result, 5)
}

func TestImporterCapsMalformedRecordDetails(t *testing.T) {
	const recordCount = 105
	importer := newImporter(&recordingImportRepo{}, &recordingPeerCache{}, nil, nil, fixedClock)

	result, err := importer.Import(testCtx(), importdb.ConflictPolicyFail, strings.NewReader(strings.Repeat("{bad json}\n", recordCount)))

	require.NoError(t, err)
	assert.Len(t, result.Errors, 100)
	assertImportErrorsOmitted(t, result, 5)
}

func assertImportErrorsOmitted(t *testing.T, result any, want float64) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(encoded, &response))
	assert.Equal(t, want, response["errorsOmitted"])
}

func TestImportTemplateRejectsInvalidEnums(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name:    "purpose",
			data:    `{"title":"Title","purpose":"sms","body":"Body","contentType":"text","labels":["first"],"audience":"patron"}`,
			wantErr: `"/purpose"`,
		},
		{
			name:    "content type",
			data:    `{"title":"Title","purpose":"email","body":"Body","contentType":"text/plain","labels":["first"],"audience":"patron"}`,
			wantErr: `"/contentType"`,
		},
		{
			name:    "audience",
			data:    `{"title":"Title","purpose":"email","body":"Body","contentType":"text","labels":["first"],"audience":"external"}`,
			wantErr: `"/audience"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingImportRepo{}
			cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
			importer := newImporter(repo, cache, nil, nil, fixedClock)

			_, _, err := importer.importTemplate(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", json.RawMessage(tt.data))

			require.ErrorContains(t, err, tt.wantErr)
			assert.Zero(t, repo.templateCalls)
		})
	}
}

func TestImportTemplateRejectsMissingOrEmptyRequiredValues(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "missing title", data: `{"purpose":"email","body":"Body","contentType":"text","labels":["first"]}`},
		{name: "empty title", data: `{"title":"","purpose":"email","body":"Body","contentType":"text","labels":["first"]}`},
		{name: "missing purpose", data: `{"title":"Title","body":"Body","contentType":"text","labels":["first"]}`},
		{name: "missing body", data: `{"title":"Title","purpose":"email","contentType":"text","labels":["first"]}`},
		{name: "empty body", data: `{"title":"Title","purpose":"email","body":"","contentType":"text","labels":["first"]}`},
		{name: "missing content type", data: `{"title":"Title","purpose":"email","body":"Body","labels":["first"]}`},
		{name: "missing labels", data: `{"title":"Title","purpose":"email","body":"Body","contentType":"text"}`},
		{name: "empty labels", data: `{"title":"Title","purpose":"email","body":"Body","contentType":"text","labels":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingImportRepo{}
			cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
			importer := newImporter(repo, cache, nil, nil, fixedClock)

			_, _, err := importer.importTemplate(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", json.RawMessage(tt.data))

			require.Error(t, err)
			assert.Zero(t, repo.templateCalls)
		})
	}
}

func TestImportTemplateAllowsMissingAudience(t *testing.T) {
	repo := &recordingImportRepo{templateResults: []importdb.Result{{Outcome: importdb.OutcomeImported}}}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
	importer := newImporter(repo, cache, nil, nil, fixedClock)

	_, _, err := importer.importTemplate(
		testCtx(),
		importdb.ConflictPolicyFail,
		"ISIL:OWNER",
		json.RawMessage(`{"title":"Title","purpose":"email","body":"Body","contentType":"text","labels":["first"]}`),
	)

	require.NoError(t, err)
	assert.Equal(t, 1, repo.templateCalls)
	assert.False(t, repo.template.Audience.Valid)
}

func TestImportTemplateRejectsEmptyLabel(t *testing.T) {
	repo := &recordingImportRepo{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
	importer := newImporter(repo, cache, nil, nil, fixedClock)

	_, _, err := importer.importTemplate(
		testCtx(),
		importdb.ConflictPolicyFail,
		"ISIL:OWNER",
		json.RawMessage(`{"title":"Title","purpose":"email","body":"Body","contentType":"text","labels":[""],"audience":"patron"}`),
	)

	require.ErrorContains(t, err, "labels")
	assert.Zero(t, repo.templateCalls)
}

func TestImporterAcceptsRecordAtSizeLimit(t *testing.T) {
	repo := &recordingImportRepo{}
	importer := newImporter(repo, &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}, nil, nil, fixedClock)
	importer.maxRecordBytes = 256
	prefix := `{"type":"template","owner":"ISIL:OWNER","data":{"title":"Title","purpose":"email","body":"`
	suffix := `","contentType":"text","labels":["first"],"audience":"patron"}}`
	record := prefix + strings.Repeat("x", 256-len(prefix)-len(suffix)) + suffix

	result, err := importer.Import(testCtx(), importdb.ConflictPolicyFail, strings.NewReader(record+"\n"))

	require.NoError(t, err)
	assert.Equal(t, int32(1), result.Templates.Imported)
}

func TestImporterRejectsRecordOverSizeLimit(t *testing.T) {
	repo := &recordingImportRepo{}
	importer := newImporter(repo, &recordingPeerCache{}, nil, nil, fixedClock)
	importer.maxRecordBytes = 128

	_, err := importer.Import(testCtx(), importdb.ConflictPolicyFail, strings.NewReader(strings.Repeat("x", 129)+"\n"))

	assert.ErrorIs(t, err, ErrImportRecordTooLarge)
	assert.Zero(t, repo.patronCalls)
	assert.Zero(t, repo.templateCalls)
}

func TestImporterForwardsPolicyToBatchAction(t *testing.T) {
	repo := &recordingImportRepo{batchResult: importdb.Result{Outcome: importdb.OutcomeImported}}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "only-one"}}}
	importer := newImporter(repo, cache, nil, nil, fixedClock)
	_, _, err := importer.importBatchAction(testCtx(), importdb.ConflictPolicyUpdate, "ISIL:OWNER", validBatchActionData())
	require.NoError(t, err)
	assert.Equal(t, importdb.ConflictPolicyUpdate, repo.batchPolicy)
	assert.Equal(t, "ISIL:OWNER", repo.batch.Owner)
	assert.Equal(t, pgText("Daily aging"), repo.batch.Title)
}

func TestImportBatchActionRejectsMissingOrEmptyRequiredValues(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "missing title", data: `{"actionName":"request-aging","batchQuery":"state==NEW","schedule":"FREQ=DAILY"}`},
		{name: "empty title", data: `{"actionName":"request-aging","batchQuery":"state==NEW","schedule":"FREQ=DAILY","title":""}`},
		{name: "missing action name", data: `{"batchQuery":"state==NEW","schedule":"FREQ=DAILY","title":"Daily aging"}`},
		{name: "missing batch query", data: `{"actionName":"request-aging","schedule":"FREQ=DAILY","title":"Daily aging"}`},
		{name: "empty batch query", data: `{"actionName":"request-aging","batchQuery":"","schedule":"FREQ=DAILY","title":"Daily aging"}`},
		{name: "missing schedule", data: `{"actionName":"request-aging","batchQuery":"state==NEW","title":"Daily aging"}`},
		{name: "empty schedule", data: `{"actionName":"request-aging","batchQuery":"state==NEW","schedule":"","title":"Daily aging"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingImportRepo{}
			cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
			importer := newImporter(repo, cache, nil, nil, fixedClock)

			_, _, err := importer.importBatchAction(testCtx(), importdb.ConflictPolicyFail, "ISIL:OWNER", json.RawMessage(tt.data))

			require.Error(t, err)
			assert.Zero(t, repo.batchCalls)
		})
	}
}

func TestImportBatchActionRejectsInvalidActionName(t *testing.T) {
	repo := &recordingImportRepo{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
	importer := newImporter(repo, cache, nil, nil, fixedClock)

	_, _, err := importer.importBatchAction(
		testCtx(),
		importdb.ConflictPolicyFail,
		"ISIL:OWNER",
		json.RawMessage(`{"actionName":"unknown","batchQuery":"state==NEW","schedule":"FREQ=DAILY","title":"Daily aging"}`),
	)

	require.Error(t, err)
	assert.Zero(t, repo.batchCalls)
}

func TestImportBatchActionRejectsInvalidSchedule(t *testing.T) {
	repo := &recordingImportRepo{}
	cache := &recordingPeerCache{peers: []ill_db.Peer{{ID: "owner-peer"}}}
	importer := newImporter(repo, cache, nil, nil, fixedClock)

	_, _, err := importer.importBatchAction(
		testCtx(),
		importdb.ConflictPolicyFail,
		"ISIL:OWNER",
		json.RawMessage(`{"actionName":"request-aging","batchQuery":"state==NEW","schedule":"not-a-rule","title":"Daily aging"}`),
	)

	require.Error(t, err)
	assert.Zero(t, repo.batchCalls)
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
	batchCalls      int
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
	r.batch, r.batchPolicy, r.batchCalls = params, policy, r.batchCalls+1
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
func fixedClock() time.Time              { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) }
func fixedTime(value string) time.Time   { parsed, _ := time.Parse(time.RFC3339, value); return parsed }
func pgText(value string) pgtype.Text    { return pgtype.Text{String: value, Valid: true} }
func stringPointer(value string) *string { return &value }

func validPatronBundleData() json.RawMessage {
	return json.RawMessage(`{
      "patronRequest":{"id":"pr-1","createdAt":"2026-08-01T10:00:00Z","updatedAt":"2026-08-02T10:00:00Z","illRequest":{"header":{"requestingAgencyRequestId":"pr-1"},"serviceInfo":{"serviceType":"Loan"}},"state":"SENT","side":"borrowing","requesterSymbol":"ISIL:REQ","requesterRequestId":"request-1","needsAttention":false,"stateModel":"default"},
      "items":[{"id":"item-1","barcode":"barcode-1","lmsRequestId":"lms-1","lmsItemId":"lms-item-1","createdAt":"2026-08-01T10:01:00Z"}],
      "notifications":[{"id":"note-1","fromSymbol":"ISIL:REQ","toSymbol":"ISIL:SUP","direction":"sent","kind":"note","cost":1.25,"createdAt":"2026-08-01T10:02:00Z","acknowledgedAt":"2026-08-01T10:03:00Z"}],
      "illTransaction":{"id":"ill-1","timestamp":"2026-08-01T10:00:00Z","requesterSymbol":"ISIL:REQ","requesterRequestID":"request-1","supplierSymbol":"ISIL:SUP","illTransactionData":{"bibliographicInfo":{}}},
      "locatedSuppliers":[{"id":"located-1","supplierSymbol":"ISIL:SUP","ordinal":1,"supplierStatus":"selected","localSupplier":false}]
    }`)
}

func mutatePatronBundleData(t *testing.T, mutate func(map[string]any)) json.RawMessage {
	t.Helper()
	var bundle map[string]any
	require.NoError(t, json.Unmarshal(validPatronBundleData(), &bundle))
	mutate(bundle)
	data, err := json.Marshal(bundle)
	require.NoError(t, err)
	return data
}

func validTemplateData() json.RawMessage {
	return json.RawMessage(`{"title":"Title","purpose":"email","body":"Body","contentType":"text","labels":["first"],"audience":"patron"}`)
}
func validBatchActionData() json.RawMessage {
	return json.RawMessage(`{"actionName":"request-aging","batchQuery":"state==NEW","schedule":"FREQ=DAILY;BYHOUR=6;BYMINUTE=0","title":"Daily aging"}`)
}
