package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConflictPolicy(t *testing.T) {
	tests := map[string]ConflictPolicy{
		"":       ConflictPolicyFail,
		"fail":   ConflictPolicyFail,
		"skip":   ConflictPolicySkip,
		"update": ConflictPolicyUpdate,
	}
	for input, expected := range tests {
		actual, err := ParseConflictPolicy(input)
		require.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
	_, err := ParseConflictPolicy("replace")
	require.EqualError(t, err, "unknown conflict policy: replace")
}

func TestEntryAggregateNormalizesAndRequiresKeySymbol(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Key = SymbolRef{Authority: "isil", Symbol: "missing"}

	err := aggregate.NormalizeAndValidate()

	require.EqualError(t, err, "entry key ISIL:MISSING must appear exactly once in symbols")
	assert.Equal(t, SymbolRef{Authority: "ISIL", Symbol: "MISSING"}, aggregate.Key)
}

func TestEntryAggregateRejectsDuplicateNormalizedSymbols(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Data.Symbols = append(aggregate.Data.Symbols, SymbolRef{Authority: "isil", Symbol: "lib"})

	err := aggregate.NormalizeAndValidate()

	require.EqualError(t, err, "duplicate entry symbol ISIL:LIB")
}

func TestTierAggregateRejectsInvalidEnum(t *testing.T) {
	aggregate := TierAggregate{
		Key:  TierKey{Consortium: SymbolRef{Authority: "isil", Symbol: "consortium"}, Name: "Loan"},
		Data: TierData{Level: "instant", Type: "loan", Entries: []SymbolRef{}},
	}

	err := aggregate.NormalizeAndValidate()

	require.EqualError(t, err, "invalid tier level: instant")
}

func TestNetworkAggregateRejectsDuplicateEntries(t *testing.T) {
	aggregate := NetworkAggregate{
		Key: NetworkKey{Consortium: SymbolRef{Authority: "isil", Symbol: "consortium"}, Name: "Main"},
		Data: NetworkData{Entries: []NetworkAssignment{
			{SymbolRef: SymbolRef{Authority: "isil", Symbol: "lib"}, Priority: 1},
			{SymbolRef: SymbolRef{Authority: "ISIL", Symbol: "LIB"}, Priority: 2},
		}},
	}

	err := aggregate.NormalizeAndValidate()

	require.EqualError(t, err, "duplicate network entry ISIL:LIB")
}

func TestEntryAggregateRejectsInvalidClosureRange(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Data.Closures = []Closure{{StartDate: "2026-09-03", EndDate: "2026-09-02", Reason: "maintenance"}}

	err := aggregate.NormalizeAndValidate()

	require.EqualError(t, err, "closure 1 endDate must not precede startDate")
}

func validEntryAggregate() EntryAggregate {
	return EntryAggregate{
		Key: SymbolRef{Authority: "isil", Symbol: "lib"},
		Data: EntryData{
			Name:    "Library",
			Type:    "Institution",
			Symbols: []SymbolRef{{Authority: "isil", Symbol: "lib"}},
		},
	}
}
