package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type diagnosticLookup struct {
	catalog.MockLookupAdapter
	query     string
	err       error
	onLookup  func()
	nilResult bool
}

type diagnosticLookupResult struct {
	catalog.LookupResult
	query string
}

func (l *diagnosticLookup) Lookup(params catalog.LookupParams) (catalog.LookupResult, error) {
	if l.onLookup != nil {
		l.onLookup()
	}
	if l.nilResult {
		return nil, l.err
	}
	result, err := l.MockLookupAdapter.Lookup(params)
	if err != nil {
		return result, err
	}
	return diagnosticLookupResult{LookupResult: result, query: l.query}, l.err
}

func (r diagnosticLookupResult) GetQuery() string { return r.query }

func diagnosticRepo() *availabilityPolicyRepo {
	return &availabilityPolicyRepo{
		peer: ill_db.Peer{CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{Sru: &dirapi.SruConfig{Address: "https://catalog.example/sru"}},
			HoldingsPolicy: &dirapi.HoldingsPolicy{ShelvingLocations: &[]dirapi.HoldingsShelvingLocation{
				{Code: "Main Library", SupplyPreference: 10},
				{Code: "Popular Reading Collection", SupplyPreference: 20},
				{Code: "Annex", SupplyPreference: -1},
			}},
		}},
		supplier: ill_db.LocatedSupplier{ID: "supplier", SupplierID: "peer", SupplierSymbol: "ISIL:US-RS3",
			IllTransactionID: "ill", SupplierStatus: ill_db.SupplierStateSelectedPg,
			LocalID:                pgtype.Text{String: "inst000000000008", Valid: true},
			HoldingsPolicyResolved: pgtype.Bool{Bool: true, Valid: true}},
	}
}

func runDiagnosticAvailability(repo *availabilityPolicyRepo, lookup catalog.LookupAdapter) (events.EventStatus, *events.EventResult) {
	factory := NewLookupAdapterFactory(repo, nil, "", nil, policyLookupCreator{lookup: lookup})
	locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, nil, factory)
	return locator.checkAvailability(appCtx, events.Event{IllTransactionID: "ill"})
}

func TestAvailabilityEventExplainsPolicySelection(t *testing.T) {
	repo := diagnosticRepo()
	lookup := &diagnosticLookup{query: `@attr 1=12 "inst000000000008"`, MockLookupAdapter: catalog.MockLookupAdapter{
		Holdings: []catalog.Holding{
			{ItemId: "MAIN", Location: "Datalogisk Institut", ShelvingLocation: "Main Library"},
			{ItemId: "POPULAR", Location: "Datalogisk Institut", ShelvingLocation: "Popular Reading Collection"},
			{ItemId: "ANNEX", Location: "Datalogisk Institut", ShelvingLocation: "Annex"},
		},
	}}
	status, result := runDiagnosticAvailability(repo, lookup)
	require.Equal(t, events.EventStatusSuccess, status)
	availability := result.CustomData["availabilityLookup"].(*availabilityLookupDetails)
	require.Equal(t, "ISIL:US-RS3", availability.SupplierSymbol)
	require.Equal(t, "inst000000000008", availability.BibliographicID)
	require.Equal(t, lookup.query, availability.Query)
	require.Equal(t, "sru", availability.Protocol)
	require.Equal(t, "https://catalog.example/sru", availability.Target)
	require.True(t, *availability.recordFound)
	require.Equal(t, 3, *availability.AvailableHoldings)
	require.True(t, availability.ResultApplied)
	require.NotEmpty(t, result.CustomData[ill_db.AvailabilityTokenKey])
	require.Empty(t, availability.AttemptToken)
	policy := result.CustomData["holdingsPolicy"].(*availabilityPolicyDetails)
	require.Equal(t, "applied", policy.Status)
	require.Equal(t, 2, policy.EligibleCount)
	require.Equal(t, 1, policy.ExcludedCount)
	require.Len(t, policy.Candidates, 3)
	require.Equal(t, 10, policy.Candidates[0].ShelvingPreference)
	require.Equal(t, 20, policy.Candidates[1].ShelvingPreference)
	require.Equal(t, "shelving-location-excluded", policy.Candidates[2].Reason)
	require.True(t, policy.Candidates[1].Selected)
	require.Equal(t, "POPULAR", policy.selected.ItemID)
	require.Equal(t, "higher-shelving-preference", policy.SelectionReason)
	require.Equal(t, policy.selected.ItemID, repo.supplier.ItemID.String)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	custom := decoded["customData"].(map[string]any)
	policyJSON := custom["holdingsPolicy"].(map[string]any)
	require.NotContains(t, policyJSON, "applied")
	require.NotContains(t, policyJSON, "candidateCount")
	require.NotContains(t, policyJSON, "selected")
	require.NotContains(t, policyJSON, "candidatesTruncated")
	require.NotContains(t, custom["availabilityLookup"].(map[string]any), "recordFound")
	require.NotContains(t, custom["availabilityLookup"].(map[string]any), "attemptToken")
	candidates := policyJSON["candidates"].([]any)
	require.NotContains(t, candidates[0].(map[string]any), "reason")
	require.NotContains(t, candidates[0].(map[string]any), "selected")
	require.Contains(t, result.Note, "Found 3 available items.")
	require.Contains(t, result.Note, "Holdings policy selected POPULAR at Popular Reading Collection: shelving preference 20, compared with Main Library's 10.")
	require.Equal(t, float64(3), custom["availabilityLookup"].(map[string]any)["availableHoldings"])
}

func TestAvailabilityEventOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, lookupOutcome, policyStatus string
		configure                         func(*availabilityPolicyRepo, *diagnosticLookup)
		status                            events.EventStatus
		applied                           bool
	}{
		{"no record", "no-record", "applied", func(_ *availabilityPolicyRepo, l *diagnosticLookup) { l.Holdings = nil }, events.EventStatusSuccess, true},
		{"record without available holdings", "no-available-holdings", "applied", func(_ *availabilityPolicyRepo, l *diagnosticLookup) { l.Holdings = nil; l.RecordFound = true }, events.EventStatusSuccess, true},
		{"all excluded", "available-holdings", "applied", func(_ *availabilityPolicyRepo, l *diagnosticLookup) { l.Holdings[0].ShelvingLocation = "Annex" }, events.EventStatusSuccess, true},
		{"no policy", "available-holdings", "not-configured", func(r *availabilityPolicyRepo, _ *diagnosticLookup) { r.peer.CustomData.HoldingsPolicy = nil }, events.EventStatusSuccess, true},
		{"lookup error keeps query", "lookup-failed", "pending", func(_ *availabilityPolicyRepo, l *diagnosticLookup) { l.err = errors.New("lookup failed") }, events.EventStatusError, false},
		{"parser error during lookup", "holdings-failed", "pending", func(_ *availabilityPolicyRepo, l *diagnosticLookup) {
			l.err = fmt.Errorf("record parsing: %w", catalog.ErrHoldingsParse)
		}, events.EventStatusError, false},
		{"holdings error", "holdings-failed", "pending", func(_ *availabilityPolicyRepo, l *diagnosticLookup) { l.HoldingsErr = errors.New("bad record") }, events.EventStatusError, false},
		{"empty result", "lookup-failed", "pending", func(_ *availabilityPolicyRepo, l *diagnosticLookup) { l.nilResult = true }, events.EventStatusError, false},
		{"commit failure", "available-holdings", "evaluated", func(r *availabilityPolicyRepo, l *diagnosticLookup) {
			l.onLookup = func() { r.commitErr = errors.New("commit failed") }
		}, events.EventStatusError, false},
		{"superseded result", "available-holdings", "evaluated", func(r *availabilityPolicyRepo, l *diagnosticLookup) {
			l.onLookup = func() { r.supplier.SupplierStatus = ill_db.SupplierStateSkippedPg }
		}, events.EventStatusSuccess, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := diagnosticRepo()
			lookup := &diagnosticLookup{query: "identifier-query", MockLookupAdapter: catalog.MockLookupAdapter{
				Holdings: []catalog.Holding{{ItemId: "copy", ShelvingLocation: "Main Library"}},
			}}
			tc.configure(repo, lookup)
			status, result := runDiagnosticAvailability(repo, lookup)
			require.Equal(t, tc.status, status)
			availability := result.CustomData["availabilityLookup"].(*availabilityLookupDetails)
			policy := result.CustomData["holdingsPolicy"].(*availabilityPolicyDetails)
			require.Equal(t, tc.lookupOutcome, availability.Outcome)
			require.Equal(t, tc.applied, availability.ResultApplied)
			require.Equal(t, tc.policyStatus, policy.Status)
			require.NotEmpty(t, result.Note)
			if tc.lookupOutcome == "holdings-failed" {
				require.Contains(t, result.Note, "Could not parse holdings lookup results.")
			}
			if tc.name == "superseded result" {
				require.Equal(t, "superseded", result.CustomData[AvailabilityKey])
				require.NotNil(t, policy.selected, "retain diagnostics without claiming the decision was applied")
				require.Contains(t, result.Note, "Result superseded")
				require.Contains(t, result.Note, "not applied")
				require.NotEmpty(t, availability.AttemptToken)
			}
			if tc.name == "all excluded" {
				require.Equal(t, "unavailable", result.CustomData[AvailabilityKey])
				require.Equal(t, "all-holdings-excluded", policy.SelectionReason)
				require.Equal(t, 1, *availability.AvailableHoldings)
				require.Equal(t, 1, policy.ExcludedCount)
			}
			if tc.name == "lookup error keeps query" {
				require.Equal(t, lookup.query, availability.Query)
				require.Nil(t, availability.recordFound)
			}
		})
	}
}

func TestAvailabilityEventWithoutLookup(t *testing.T) {
	repo := diagnosticRepo()
	repo.peer.CustomData.CatalogConfig = nil
	status, result := runDiagnosticAvailability(repo, nil)
	require.Equal(t, events.EventStatusSuccess, status)
	require.Equal(t, "not-configured", result.CustomData["availabilityLookup"].(*availabilityLookupDetails).Outcome)
	require.Equal(t, "applied-at-discovery", result.CustomData["holdingsPolicy"].(*availabilityPolicyDetails).Status)
	require.Contains(t, result.Note, "Holdings policy was applied during discovery.")
}

func TestAvailabilityPolicyDiagnosticsBoundedAndDeterministic(t *testing.T) {
	entry := diagnosticRepo().peer.CustomData
	holdings := make([]catalog.Holding, availabilityCandidateLogLimit+10)
	for i := range holdings {
		holdings[i] = catalog.Holding{ItemId: fmt.Sprintf("copy-%d", i), ShelvingLocation: "Main Library"}
	}
	holdings[len(holdings)-1].ShelvingLocation = "Popular Reading Collection"
	details := new(availabilityPolicyDetails)
	selected := details.evaluate(entry, holdings)
	require.Len(t, details.Candidates, availabilityCandidateLogLimit)
	require.Equal(t, 10, details.CandidatesTruncated)
	require.Equal(t, len(holdings), details.EligibleCount+details.ExcludedCount)
	require.Equal(t, len(holdings), details.EligibleCount)
	require.Equal(t, selected.ItemID, details.selected.ItemID)
	require.True(t, details.selected.Selected)
	require.Equal(t, selected.ItemID, details.Candidates[len(details.Candidates)-1].ItemID)
	selectedCount := 0
	for _, candidate := range details.Candidates {
		if candidate.Selected {
			selectedCount++
		}
	}
	require.Equal(t, 1, selectedCount)
	require.Equal(t, "higher-shelving-preference", details.SelectionReason)
	details = new(availabilityPolicyDetails)
	selected = details.evaluate(entry, holdings[:2])
	require.Equal(t, "copy-0", selected.ItemID)
	require.Equal(t, "equal-preference-first-returned", details.SelectionReason)
}

func TestAvailabilityDiagnosticRedactionAndTextBounds(t *testing.T) {
	for _, address := range []string{
		"https://user:secret@catalog.example/sru?apiKey=secret#secret",
		"user:secret@catalog.example:210/db?password=secret#secret",
		"https://user:secret@%broken",
	} {
		target := diagnosticTarget(address)
		require.NotContains(t, target, "secret")
		require.NotContains(t, target, "user")
	}
	require.Equal(t, "tcp://catalog.example:210/db", diagnosticTarget("catalog.example:210/db"))
	text := diagnosticText(strings.Repeat("x", availabilityDiagnosticTextLimit-1) + strings.Repeat("é", 10))
	require.True(t, utf8.ValidString(text))
	require.LessOrEqual(t, len(text), availabilityDiagnosticTextLimit+len("…"))
	require.True(t, strings.HasSuffix(text, "…"))
}

func TestOperatorPolicySummary(t *testing.T) {
	entry := diagnosticRepo().peer.CustomData
	entry.HoldingsPolicy.Locations = &[]dirapi.HoldingsLocation{{Code: "Preferred", SupplyPreference: 5}}
	for _, tc := range []struct {
		name         string
		holdings     []catalog.Holding
		status, want string
	}{
		{"location preference with identical shelving names", []catalog.Holding{{ItemId: "other", Location: "Other", ShelvingLocation: "Main Library"}, {ItemId: "winner", Location: "Preferred", ShelvingLocation: "Main Library"}}, "applied", "Holdings policy selected winner at Main Library: location preference 5 for Preferred, compared with Other's 0."},
		{"location preference with missing location", []catalog.Holding{{ItemId: "other", ShelvingLocation: "Main Library"}, {ItemId: "winner", Location: "Preferred", ShelvingLocation: "Main Library"}}, "applied", "location preference 5 for Preferred, compared with unspecified location's 0."},
		{"tie", []catalog.Holding{{ItemId: "first"}, {ItemId: "second"}}, "applied", "preferences tied"},
		{"no item ID", []catalog.Holding{{Location: "Preferred"}}, "applied", "selected an available holding at Preferred"},
		{"unspecified location", []catalog.Holding{{ItemId: "copy"}}, "applied", "at unspecified location"},
		{"failed save", []catalog.Holding{{ItemId: "copy"}}, "evaluated", "would select copy"},
		{"rejected", []catalog.Holding{{ShelvingLocation: "Annex"}}, "evaluated", "would exclude all 1 available item"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := new(availabilityPolicyDetails)
			d.evaluate(entry, tc.holdings)
			d.Status = tc.status
			require.Contains(t, d.summary(), tc.want)
		})
	}
}
