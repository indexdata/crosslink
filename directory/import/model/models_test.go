package model

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseConflictPolicy(t *testing.T) {
	for input, expected := range map[string]ConflictPolicy{"": ConflictPolicyFail, "fail": ConflictPolicyFail, "skip": ConflictPolicySkip, "update": ConflictPolicyUpdate} {
		actual, err := ParseConflictPolicy(input)
		require.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
	_, err := ParseConflictPolicy("replace")
	require.EqualError(t, err, "unknown conflict policy: replace")
}

func TestEntryAggregateRequiresUUID(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Key = uuid.Nil
	require.EqualError(t, aggregate.NormalizeAndValidate(), "entry key must be a valid UUID")
}

func TestEntryAggregateRejectsDuplicateNormalizedSymbols(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Data.Symbols = append(aggregate.Data.Symbols, SymbolRef{Authority: "isil", Symbol: "lib"})
	require.EqualError(t, aggregate.NormalizeAndValidate(), "duplicate entry symbol ISIL:LIB")
}

func TestEntryAggregateAcceptsUUIDIndependentOfSymbols(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Data.Symbols = []SymbolRef{{Authority: "ISIL", Symbol: "OTHER"}}
	require.NoError(t, aggregate.NormalizeAndValidate())
}

func TestEntryAggregateRejectsNilLender(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Data.ILLConfig = &ILLConfig{LendersOfLastResort: []uuid.UUID{uuid.Nil}}
	require.EqualError(t, aggregate.NormalizeAndValidate(), "lender of last resort 1 must be a valid UUID")
}

func TestTierAggregateRejectsInvalidEnum(t *testing.T) {
	aggregate := TierAggregate{Key: TierKey{Consortium: uuid.New(), Name: "Loan"}, Data: TierData{Level: "instant", Type: "loan", Entries: []uuid.UUID{}}}
	require.EqualError(t, aggregate.NormalizeAndValidate(), "invalid tier level: instant")
}

func TestTierAggregateRejectsNilEntry(t *testing.T) {
	aggregate := TierAggregate{Key: TierKey{Consortium: uuid.New(), Name: "Loan"}, Data: TierData{Level: "standard", Type: "loan", Entries: []uuid.UUID{uuid.Nil}}}
	require.EqualError(t, aggregate.NormalizeAndValidate(), "tier entry 1 must be a valid UUID")
}

func TestNetworkAggregateRejectsDuplicateEntries(t *testing.T) {
	id := uuid.New()
	aggregate := NetworkAggregate{Key: NetworkKey{Consortium: uuid.New(), Name: "Main"}, Data: NetworkData{Entries: []NetworkAssignment{{Entry: id, Priority: 1}, {Entry: id, Priority: 2}}}}
	require.EqualError(t, aggregate.NormalizeAndValidate(), "duplicate network entry "+id.String())
}

func TestNetworkAggregateRejectsNilEntry(t *testing.T) {
	aggregate := NetworkAggregate{Key: NetworkKey{Consortium: uuid.New(), Name: "Main"}, Data: NetworkData{Entries: []NetworkAssignment{{Entry: uuid.Nil, Priority: 1}}}}
	require.EqualError(t, aggregate.NormalizeAndValidate(), "network entry 1 must be a valid UUID")
}

func TestEntryAggregateRejectsInvalidClosureRange(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Data.Closures = []Closure{{StartDate: "2026-09-03", EndDate: "2026-09-02", Reason: "maintenance"}}
	require.EqualError(t, aggregate.NormalizeAndValidate(), "closure 1 endDate must not precede startDate")
}

func validEntryAggregate() EntryAggregate {
	return EntryAggregate{Key: uuid.New(), Data: EntryData{Name: "Library", Type: "Institution", Symbols: []SymbolRef{{Authority: "isil", Symbol: "lib"}}}}
}
