package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingRepo struct {
	entryCalls   int
	tierCalls    int
	networkCalls int
	result       model.RepoResult
	err          error
}

func (r *recordingRepo) ImportEntry(context.Context, model.EntryAggregate, model.ConflictPolicy) (model.RepoResult, error) {
	r.entryCalls++
	return r.result, r.err
}

func (r *recordingRepo) ImportTier(context.Context, model.TierAggregate, model.ConflictPolicy) (model.RepoResult, error) {
	r.tierCalls++
	return r.result, r.err
}

func (r *recordingRepo) ImportNetwork(context.Context, model.NetworkAggregate, model.ConflictPolicy) (model.RepoResult, error) {
	r.networkCalls++
	return r.result, r.err
}

func TestImportDispatchesAllAggregateTypes(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	input := strings.Join([]string{validEntryRecord(), validTierRecord(), validNetworkRecord()}, "\n")

	result, err := New(repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(input))

	require.NoError(t, err)
	assert.Equal(t, model.ImportSectionResult{Imported: 1}, result.Entries)
	assert.Equal(t, model.ImportSectionResult{Imported: 1}, result.Tiers)
	assert.Equal(t, model.ImportSectionResult{Imported: 1}, result.Networks)
	assert.Empty(t, result.Errors)
	assert.Equal(t, 1, repo.entryCalls)
	assert.Equal(t, 1, repo.tierCalls)
	assert.Equal(t, 1, repo.networkCalls)
}

func TestImportContinuesAfterMalformedRecord(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	input := "{bad json}\n\n" + validEntryRecord() + "\n"

	result, err := New(repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(input))

	require.NoError(t, err)
	assert.Zero(t, result.Entries.Failed)
	assert.Equal(t, int32(1), result.Entries.Imported)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, int32(1), result.Errors[0].Line)
	assert.Nil(t, result.Errors[0].Type)
	assert.NotContains(t, result.Errors[0].Error, "{bad json}")
	assert.Equal(t, 1, repo.entryCalls)
}

func TestImportContinuesAfterSemanticFailure(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	badTier := strings.Replace(validTierRecord(), `"level":"standard"`, `"level":"invalid"`, 1)

	result, err := New(repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(badTier+"\n"+validTierRecord()))

	require.NoError(t, err)
	assert.Equal(t, model.ImportSectionResult{Imported: 1, Failed: 1}, result.Tiers)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, int32(1), result.Errors[0].Line)
	require.NotNil(t, result.Errors[0].Type)
	assert.Equal(t, "tier", *result.Errors[0].Type)
	assert.Equal(t, 1, repo.tierCalls)
}

func TestImportBlankLinesDoNotIncrementRecordNumber(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	badTier := strings.Replace(validTierRecord(), `"level":"standard"`, `"level":"invalid"`, 1)
	input := "\n" + validTierRecord() + "\n\r\n" + badTier

	result, err := New(repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(input))

	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	require.Equal(t, int32(2), result.Errors[0].Line)
}

func TestImportReturnsFatalReaderError(t *testing.T) {
	fatal := errors.New("transport failed")
	result, err := New(&recordingRepo{}).Import(context.Background(), model.ConflictPolicyFail, errorReader{err: fatal})

	require.ErrorIs(t, err, fatal)
	require.Empty(t, result.Errors)
}

func TestImportRejectsMissingAndUnknownProperties(t *testing.T) {
	tests := map[string]string{
		"missing entry field": strings.Replace(validEntryRecord(), `,"timeZone":null`, "", 1),
		"unknown envelope":    strings.Replace(validTierRecord(), `"type":"tier"`, `"type":"tier","secret":"do-not-echo"`, 1),
		"null collection":     strings.Replace(validEntryRecord(), `"endpoints":[]`, `"endpoints":null`, 1),
		"missing child field": strings.Replace(
			strings.Replace(validEntryRecord(), `"endpoints":[]`, `"endpoints":[{"name":"ISO","type":"ISO18626","address":"https://example.test"}]`, 1),
			`,"address":"https://example.test"`, "", 1),
		"missing config field": strings.Replace(
			strings.Replace(validEntryRecord(), `"illConfig":null`, validILLConfig(), 1),
			`,"supplierPatronPattern":null`, "", 1),
	}
	for name, record := range tests {
		t.Run(name, func(t *testing.T) {
			repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
			result, err := New(repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record))
			require.NoError(t, err)
			require.Len(t, result.Errors, 1)
			assert.NotContains(t, result.Errors[0].Error, "do-not-echo")
			assert.Zero(t, repo.entryCalls+repo.tierCalls+repo.networkCalls)
		})
	}
}

func validILLConfig() string {
	return `"illConfig":{"iso18626Url":null,"iso18626Vendor":null,"lendersOfLastResort":[],"includeRequestingAgencyInfo":null,"includeSupplierInfo":null,"includeReturnInfo":null,"includeVendorNote":null,"useOfferedCosts":null,"noteFieldSeparator":null,"supplierPatronPattern":null,"duplicateCheckWindowHours":null}`
}

func TestImportAccountsForSkippedAndRepositoryFailures(t *testing.T) {
	t.Run("skipped", func(t *testing.T) {
		repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeSkipped, Diagnostic: "entry already exists"}}
		result, err := New(repo).Import(context.Background(), model.ConflictPolicySkip, strings.NewReader(validEntryRecord()))
		require.NoError(t, err)
		assert.Equal(t, model.ImportSectionResult{Skipped: 1}, result.Entries)
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "entry already exists", result.Errors[0].Error)
	})

	t.Run("failed", func(t *testing.T) {
		repo := &recordingRepo{err: errors.New("entry parent does not exist")}
		result, err := New(repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(validEntryRecord()))
		require.NoError(t, err)
		assert.Equal(t, model.ImportSectionResult{Failed: 1}, result.Entries)
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "entry parent does not exist", result.Errors[0].Error)
	})
}

func TestImportRecordLimitIsExact(t *testing.T) {
	record := validTierRecord()
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	importer := New(repo)
	importer.maxRecordBytes = len(record)

	result, err := importer.Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record+"\r\n"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), result.Tiers.Imported)

	repo = &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	importer = New(repo)
	importer.maxRecordBytes = len(record) - 1
	_, err = importer.Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record+"\n"))
	require.ErrorIs(t, err, ErrRecordTooLarge)
	assert.Zero(t, repo.tierCalls)
}

func validEntryRecord() string {
	return `{"type":"entry","key":{"authority":"isil","symbol":"abc"},"data":{"name":"Library","type":"Institution","parent":null,"description":null,"organizationId":null,"contactName":null,"email":null,"fromEmail":null,"tenant":null,"vendor":null,"phoneNumber":null,"lmsLocationCode":null,"hrid":null,"timeZone":null,"symbols":[{"authority":"isil","symbol":"abc"}],"endpoints":[],"addresses":[],"closures":[],"lmsConfig":null,"catalogConfig":null,"illConfig":null,"holdingsPolicy":null}}`
}

func validTierRecord() string {
	return `{"type":"tier","key":{"consortium":{"authority":"isil","symbol":"con"},"name":"Primary"},"data":{"level":"standard","type":"loan","cost":0,"entries":[]}}`
}

func validNetworkRecord() string {
	return `{"type":"network","key":{"consortium":{"authority":"isil","symbol":"con"},"name":"Main"},"data":{"priority":1,"reciprocal":null,"entries":[]}}`
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }
