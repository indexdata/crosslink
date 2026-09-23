package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingRepo struct {
	entryCalls   int
	tierCalls    int
	networkCalls int
	entry        *model.EntryAggregate
	result       model.RepoResult
	err          error
	beforeEntry  func()
}

func (r *recordingRepo) ImportEntry(_ context.Context, aggregate model.EntryAggregate, _ model.ConflictPolicy) (model.RepoResult, error) {
	r.entryCalls++
	r.entry = &aggregate
	if r.beforeEntry != nil {
		r.beforeEntry()
	}
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

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(input))

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

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(input))

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
	badTier := strings.Replace(validTierRecord(), `"name":"Primary"`, `"name":"   "`, 1)

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(badTier+"\n"+validTierRecord()))

	require.NoError(t, err)
	assert.Equal(t, model.ImportSectionResult{Imported: 1, Failed: 1}, result.Tiers)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, int32(1), result.Errors[0].Line)
	require.NotNil(t, result.Errors[0].Type)
	assert.Equal(t, "tier", *result.Errors[0].Type)
	assert.Equal(t, 1, repo.tierCalls)
}

func TestImportDoesNotEchoSchemaRejectedValues(t *testing.T) {
	for name, record := range map[string]string{
		"schema violation":    strings.Replace(validTierRecord(), `"level":"standard"`, `"level":"private-secret"`, 1),
		"unknown record type": strings.Replace(validTierRecord(), `"type":"tier"`, `"type":"private-secret"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}

			result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record))

			require.NoError(t, err)
			require.Len(t, result.Errors, 1)
			assert.NotContains(t, result.Errors[0].Error, "private-secret")
			assert.Zero(t, repo.tierCalls)
		})
	}
}

func TestImportBlankLinesDoNotIncrementRecordNumber(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	badTier := strings.Replace(validTierRecord(), `"level":"standard"`, `"level":"invalid"`, 1)
	input := "\n" + validTierRecord() + "\n\r\n" + badTier

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(input))

	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	require.Equal(t, int32(2), result.Errors[0].Line)
}

func TestImportReturnsFatalReaderError(t *testing.T) {
	fatal := errors.New("transport failed")
	result, err := newTestImporter(t, &recordingRepo{}).Import(context.Background(), model.ConflictPolicyFail, errorReader{err: fatal})

	require.ErrorIs(t, err, fatal)
	require.Empty(t, result.Errors)
}

func TestImportPropagatesRepositoryCancellation(t *testing.T) {
	for name, fatal := range map[string]error{
		"canceled": context.Canceled,
		"deadline": context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &recordingRepo{err: fatal}
			input := validEntryRecord() + "\n" + validEntryRecord()

			result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(input))

			require.ErrorIs(t, err, fatal)
			require.Equal(t, 1, repo.entryCalls)
			require.Zero(t, result.Entries.Failed)
			require.Empty(t, result.Errors)
		})
	}
}

func TestImportPropagatesCanceledContextWhenRepositoryMasksCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := &recordingRepo{err: errors.New("database operation failed"), beforeEntry: cancel}
	input := validEntryRecord() + "\n" + validEntryRecord()

	result, err := newTestImporter(t, repo).Import(ctx, model.ConflictPolicyFail, strings.NewReader(input))

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, repo.entryCalls)
	require.Zero(t, result.Entries.Failed)
	require.Empty(t, result.Errors)
}

func TestImportStopsBeforeReadingWhenContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}

	result, err := newTestImporter(t, repo).Import(ctx, model.ConflictPolicyFail, strings.NewReader(validEntryRecord()))

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, repo.entryCalls)
	require.Zero(t, result.Entries.Imported)
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
		"missing patron profiles": strings.Replace(
			strings.Replace(validEntryRecord(), `"lmsConfig":null`, validLMSConfig(), 1),
			`,"patronProfiles":[{"code":"STAFF","canCreateRequests":true}]`, "", 1),
	}
	for name, record := range tests {
		t.Run(name, func(t *testing.T) {
			repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
			result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record))
			require.NoError(t, err)
			require.Len(t, result.Errors, 1)
			assert.NotContains(t, result.Errors[0].Error, "do-not-echo")
			assert.Zero(t, repo.entryCalls+repo.tierCalls+repo.networkCalls)
		})
	}
}

func TestImportAcceptsLMSPatronProfiles(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	record := strings.Replace(validEntryRecord(), `"lmsConfig":null`, validLMSConfig(), 1)

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record))

	require.NoError(t, err)
	assert.Equal(t, model.ImportSectionResult{Imported: 1}, result.Entries)
	assert.Empty(t, result.Errors)
	assert.Equal(t, 1, repo.entryCalls)
	require.NotNil(t, repo.entry)
	require.NotNil(t, repo.entry.Data.LMSConfig)
	require.NotNil(t, repo.entry.Data.LMSConfig.PatronProfiles)
	profiles := *repo.entry.Data.LMSConfig.PatronProfiles
	require.Len(t, profiles, 1)
	require.NotNil(t, profiles[0].Code)
	require.Equal(t, "STAFF", *profiles[0].Code)
	require.True(t, profiles[0].CanCreateRequests)
}

func TestImportAcceptsNullLMSPatronProfiles(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	lmsConfig := strings.Replace(validLMSConfig(), `"patronProfiles":[{"code":"STAFF","canCreateRequests":true}]`, `"patronProfiles":null`, 1)
	record := strings.Replace(validEntryRecord(), `"lmsConfig":null`, lmsConfig, 1)

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record))

	require.NoError(t, err)
	assert.Equal(t, model.ImportSectionResult{Imported: 1}, result.Entries)
	assert.Empty(t, result.Errors)
	require.NotNil(t, repo.entry)
	require.NotNil(t, repo.entry.Data.LMSConfig)
	require.Nil(t, repo.entry.Data.LMSConfig.PatronProfiles)
}

func TestImportAcceptsMaxRequestsPerPatron(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	record := strings.Replace(validEntryRecord(), `"illConfig":null`, validILLConfig(), 1)

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record))

	require.NoError(t, err)
	assert.Equal(t, model.ImportSectionResult{Imported: 1}, result.Entries)
	assert.Empty(t, result.Errors)
	require.NotNil(t, repo.entry)
	require.NotNil(t, repo.entry.Data.ILLConfig)
	require.NotNil(t, repo.entry.Data.ILLConfig.MaxRequestsPerPatron)
	assert.Equal(t, int32(0), *repo.entry.Data.ILLConfig.MaxRequestsPerPatron)
}

func validLMSConfig() string {
	return `"lmsConfig":{"vendor":null,"ncipNamespaceEnabled":null,"bibIdNormalization":null,"address":"https://example.test/ncip","fromAgency":"FROM","fromAgencyAuthentication":null,"toAgency":null,"lookupUserEnabled":true,"acceptItemEnabled":true,"checkInItemEnabled":true,"checkOutItemEnabled":true,"itemLocation":null,"requestItemRequestType":null,"requestItemRequestScopeType":null,"requestItemBibIdCode":null,"requestItemEnabled":true,"requestItemPickupLocationEnabled":true,"requesterPickupLocation":null,"supplierPickupLocation":null,"requesterPatronPattern":null,"patronProfiles":[{"code":"STAFF","canCreateRequests":true}]}`
}

func validILLConfig() string {
	return `"illConfig":{"iso18626Url":null,"iso18626Vendor":null,"lendersOfLastResort":[],"includeRequestingAgencyInfo":null,"includeSupplierInfo":null,"includeReturnInfo":null,"includeVendorNote":null,"useOfferedCosts":null,"noteFieldSeparator":null,"supplierPatronPattern":null,"duplicateCheckWindowHours":null,"maxRequestsPerPatron":0}`
}

func TestImportAccountsForSkippedAndRepositoryFailures(t *testing.T) {
	t.Run("skipped", func(t *testing.T) {
		repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeSkipped, Diagnostic: "entry already exists"}}
		result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicySkip, strings.NewReader(validEntryRecord()))
		require.NoError(t, err)
		assert.Equal(t, model.ImportSectionResult{Skipped: 1}, result.Entries)
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "entry already exists", result.Errors[0].Error)
	})

	t.Run("failed", func(t *testing.T) {
		repo := &recordingRepo{err: errors.New("entry parent does not exist")}
		result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(validEntryRecord()))
		require.NoError(t, err)
		assert.Equal(t, model.ImportSectionResult{Failed: 1}, result.Entries)
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "entry parent does not exist", result.Errors[0].Error)
	})
}

func TestImportCapsFailureDetailsWhilePreservingCounters(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	badTier := strings.Replace(validTierRecord(), `"name":"Primary"`, `"name":"   "`, 1)
	input := strings.Repeat(badTier+"\n", 1005)

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(input))

	require.NoError(t, err)
	require.Equal(t, model.ImportSectionResult{Failed: 1005}, result.Tiers)
	require.Len(t, result.Errors, 1000)
	require.Equal(t, int32(5), result.ErrorsOmitted)
	require.Zero(t, repo.tierCalls)
}

func TestImportCapsSkippedDetailsWhilePreservingCounters(t *testing.T) {
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeSkipped, Diagnostic: "entry already exists"}}
	input := strings.Repeat(validEntryRecord()+"\n", 1005)

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicySkip, strings.NewReader(input))

	require.NoError(t, err)
	require.Equal(t, model.ImportSectionResult{Skipped: 1005}, result.Entries)
	require.Len(t, result.Errors, 1000)
	require.Equal(t, int32(5), result.ErrorsOmitted)
	require.Equal(t, 1005, repo.entryCalls)
}

func TestImportCapsRetainedErrorFieldSizes(t *testing.T) {
	repo := &recordingRepo{err: errors.New(strings.Repeat("failure", 1000))}
	longSymbol := strings.Repeat("x", 5000)
	record := strings.ReplaceAll(validEntryRecord(), `"symbol":"abc"`, `"symbol":"`+longSymbol+`"`)

	result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record))

	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	require.NotNil(t, result.Errors[0].Key)
	require.LessOrEqual(t, len(*result.Errors[0].Key), 1024)
	require.LessOrEqual(t, len(result.Errors[0].Error), 1024)
}

func TestImportDoesNotRetainUnknownRecordType(t *testing.T) {
	unknownType := strings.Repeat("x", 5000)
	record := `{"type":"` + unknownType + `","key":{},"data":{}}`

	result, err := newTestImporter(t, &recordingRepo{}).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record))

	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	require.Nil(t, result.Errors[0].Type)
}

func TestImportRecordLimitIsExact(t *testing.T) {
	record := validTierRecord()
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	importer := newTestImporter(t, repo)
	importer.maxRecordBytes = len(record)

	result, err := importer.Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(record+"\r\n"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), result.Tiers.Imported)

	repo = &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	importer = newTestImporter(t, repo)
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
	return `{"type":"network","key":{"consortium":{"authority":"isil","symbol":"con"},"name":"Main"},"data":{"reciprocal":null,"entries":[]}}`
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestNewRejectsMissingImportRecordSchema(t *testing.T) {
	_, err := New(&recordingRepo{}, &openapi3.T{Components: &openapi3.Components{Schemas: openapi3.Schemas{}}})

	require.ErrorContains(t, err, "ImportEntryRecord")
}

func TestImportUsesInjectedOpenAPIRecordSchema(t *testing.T) {
	spec := loadImportSpec(t)
	minimumCost := 1.0
	spec.Components.Schemas["ImportTierData"].Value.Properties["cost"].Value.Min = &minimumCost
	repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
	importer, err := New(repo, spec)
	require.NoError(t, err)

	result, err := importer.Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(validTierRecord()))

	require.NoError(t, err)
	assert.Equal(t, model.ImportSectionResult{Failed: 1}, result.Tiers)
	require.Len(t, result.Errors, 1)
	assert.Zero(t, repo.tierCalls)
}

func newTestImporter(t *testing.T, repository Repository) *Importer {
	t.Helper()
	spec := loadImportSpec(t)
	importer, err := New(repository, spec)
	require.NoError(t, err)
	return importer
}

func loadImportSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromFile("../../api.yaml")
	require.NoError(t, err)
	require.NoError(t, spec.Validate(context.Background()))
	return spec
}

func TestImportValidatesHostSettings(t *testing.T) {
	const catalogJSON = `{"profile":"Koha","metadataUpdateMode":null,"sru":null,"zoom":null,"queryConfig":null,"metadataFormat":null,"holdingsFormat":{"marc":{"availability":[{"subField":"7","operator":"equals","value":"0"}],"mainField":null,"callNumberSubField":null,"itemIdSubField":null,"locationSubField":null,"restrictedSubField":null,"shelvingLocationSubField":null},"opac":null,"reservoir":null,"marc21plus1":null}}`
	const opacJSON = `{"availabilityRule":"publicNote","availablePublicNotes":[],"requireLocalLocation":false,"shelvingLocationSource":"localLocation","includeItemId":false,"includeItemLoanPolicy":false,"includeTemporaryLocation":true,"allCirculations":true}`
	for _, tc := range []struct {
		name string
		edit func(lms, catalog map[string]any)
	}{
		{"unknown vendor", func(l, c map[string]any) { l["vendor"] = "bad" }},
		{"missing vendor", func(l, c map[string]any) { delete(l, "vendor") }},
		{"unknown normalization", func(l, c map[string]any) { l["bibIdNormalization"] = "bad" }},
		{"namespace type", func(l, c map[string]any) { l["ncipNamespaceEnabled"] = "false" }},
		{"unknown profile", func(l, c map[string]any) { c["profile"] = "bad" }},
		{"missing profile", func(l, c map[string]any) { delete(c, "profile") }},
		{"two endpoints", func(l, c map[string]any) {
			c["sru"] = map[string]any{"address": "https://example/sru", "recordSchema": nil}
			c["zoom"] = map[string]any{"address": "example:210", "options": nil}
		}},
		{"two parsers", func(l, c map[string]any) { c["holdingsFormat"].(map[string]any)["reservoir"] = map[string]any{} }},
		{"unknown parser option", func(l, c map[string]any) {
			c["holdingsFormat"].(map[string]any)["marc"].(map[string]any)["unknown"] = true
		}},
		{"missing availability", func(l, c map[string]any) {
			delete(c["holdingsFormat"].(map[string]any)["marc"].(map[string]any), "availability")
		}},
		{"equals without value", func(l, c map[string]any) {
			c["holdingsFormat"].(map[string]any)["marc"].(map[string]any)["availability"].([]any)[0].(map[string]any)["value"] = nil
		}},
		{"invalid predicate operator", func(l, c map[string]any) {
			c["holdingsFormat"].(map[string]any)["marc"].(map[string]any)["availability"].([]any)[0].(map[string]any)["operator"] = "bad"
		}},
		{"empty predicate subfield", func(l, c map[string]any) {
			c["holdingsFormat"].(map[string]any)["marc"].(map[string]any)["availability"].([]any)[0].(map[string]any)["subField"] = ""
		}},
		{"invalid OPAC rule", func(l, c map[string]any) {
			var opac map[string]any
			require.NoError(t, json.Unmarshal([]byte(opacJSON), &opac))
			opac["availabilityRule"] = "bad"
			h := c["holdingsFormat"].(map[string]any)
			h["marc"], h["opac"] = nil, opac
		}},
		{"invalid OPAC location", func(l, c map[string]any) {
			var opac map[string]any
			require.NoError(t, json.Unmarshal([]byte(opacJSON), &opac))
			opac["shelvingLocationSource"] = "bad"
			h := c["holdingsFormat"].(map[string]any)
			h["marc"], h["opac"] = nil, opac
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &recordingRepo{result: model.RepoResult{Outcome: model.OutcomeImported}}
			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.Replace(validEntryRecord(), `"lmsConfig":null`, validLMSConfig(), 1)), &record))
			data := record["data"].(map[string]any)
			var catalog map[string]any
			require.NoError(t, json.Unmarshal([]byte(catalogJSON), &catalog))
			data["catalogConfig"] = catalog
			tc.edit(data["lmsConfig"].(map[string]any), catalog)
			payload, err := json.Marshal(record)
			require.NoError(t, err)
			result, err := newTestImporter(t, repo).Import(context.Background(), model.ConflictPolicyFail, strings.NewReader(string(payload)))
			require.NoError(t, err)
			require.Equal(t, int32(1), result.Entries.Failed)
			require.Len(t, result.Errors, 1)
			require.Zero(t, repo.entryCalls)
		})
	}
}
