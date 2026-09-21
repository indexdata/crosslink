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

func TestEntryAggregateAcceptsDistinctSymbolsWithSameDisplayString(t *testing.T) {
	first := SymbolRef{Authority: "A:B", Symbol: "C"}
	second := SymbolRef{Authority: "A", Symbol: "B:C"}
	aggregate := validEntryAggregate()
	aggregate.Key = first
	aggregate.Data.Symbols = []SymbolRef{first, second}

	require.NoError(t, aggregate.NormalizeAndValidate())
}

func TestEntryAggregateRejectsColonInLenderAuthority(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Data.ILLConfig = &ILLConfig{
		LendersOfLastResort: []SymbolRef{{Authority: " a:b ", Symbol: " c "}},
	}

	err := aggregate.NormalizeAndValidate()

	require.EqualError(t, err, "lender of last resort 1 authority must not contain ':'")
}

func TestEntryAggregateAllowsColonInLenderSymbol(t *testing.T) {
	aggregate := validEntryAggregate()
	aggregate.Data.ILLConfig = &ILLConfig{
		LendersOfLastResort: []SymbolRef{{Authority: " a ", Symbol: " b:c "}},
	}

	require.NoError(t, aggregate.NormalizeAndValidate())
	assert.Equal(t, SymbolRef{Authority: "A", Symbol: "B:C"}, aggregate.Data.ILLConfig.LendersOfLastResort[0])
}

func TestTierAggregateRejectsInvalidEnum(t *testing.T) {
	aggregate := TierAggregate{
		Key:  TierKey{Consortium: SymbolRef{Authority: "isil", Symbol: "consortium"}, Name: "Loan"},
		Data: TierData{Level: "instant", Type: "loan", Entries: []SymbolRef{}},
	}

	err := aggregate.NormalizeAndValidate()

	require.EqualError(t, err, "invalid tier level: instant")
}

func TestTierAggregateAcceptsCopyOrLoanType(t *testing.T) {
	aggregate := TierAggregate{
		Key:  TierKey{Consortium: SymbolRef{Authority: "isil", Symbol: "consortium"}, Name: "Copy or loan"},
		Data: TierData{Level: "standard", Type: "copyorloan", Entries: []SymbolRef{}},
	}

	require.NoError(t, aggregate.NormalizeAndValidate())
}

func TestTierAggregateAcceptsDistinctEntriesWithSameDisplayString(t *testing.T) {
	aggregate := TierAggregate{
		Key: TierKey{Consortium: SymbolRef{Authority: "ISIL", Symbol: "CONSORTIUM"}, Name: "Loan"},
		Data: TierData{
			Level: "standard",
			Type:  "loan",
			Entries: []SymbolRef{
				{Authority: "A:B", Symbol: "C"},
				{Authority: "A", Symbol: "B:C"},
			},
		},
	}

	require.NoError(t, aggregate.NormalizeAndValidate())
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

func TestNetworkAggregateAcceptsDistinctEntriesWithSameDisplayString(t *testing.T) {
	aggregate := NetworkAggregate{
		Key: NetworkKey{Consortium: SymbolRef{Authority: "ISIL", Symbol: "CONSORTIUM"}, Name: "Main"},
		Data: NetworkData{Entries: []NetworkAssignment{
			{SymbolRef: SymbolRef{Authority: "A:B", Symbol: "C"}, Priority: 1},
			{SymbolRef: SymbolRef{Authority: "A", Symbol: "B:C"}, Priority: 2},
		}},
	}

	require.NoError(t, aggregate.NormalizeAndValidate())
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
