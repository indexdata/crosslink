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

func TestEntryAggregateValidatesLendToBorrowRatio(t *testing.T) {
	valid := []string{"50:2", "5.5:1", "0.5:2", "005.50:01", "9999.99:9999.99", "0.01:9999.99", "9999.99:0.01", "0000.01:0001.00", "1000:1", "0100:1", "0010:1", "0001:1"}
	for _, ratio := range valid {
		t.Run("valid "+ratio, func(t *testing.T) {
			aggregate := validEntryAggregate()
			aggregate.Data.LendToBorrowRatio = &ratio
			require.NoError(t, aggregate.NormalizeAndValidate())
		})
	}

	invalid := []string{"0:1", "1:0", "0.0:2", "-1:2", "+1:2", "1e2:1", "1 :2", ".5:1", "5.:1", "1", "1:2:3", "10000:1", "1:10000", "00001:1", "1:00001", "1.001:1", "1:1.001", "0.001:1", "1:0.001", "0.00:1", "1:0.00", "9999.99:9999.999"}
	for _, ratio := range invalid {
		t.Run("invalid "+ratio, func(t *testing.T) {
			aggregate := validEntryAggregate()
			aggregate.Data.LendToBorrowRatio = &ratio

			err := aggregate.NormalizeAndValidate()

			require.EqualError(t, err, "invalid lendToBorrowRatio: must contain two positive unsigned decimals separated by a colon, each with at most four integer digits and two decimal places")
		})
	}
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

func TestEntryAggregateLoadBalancingPolicy(t *testing.T) {
	for _, policy := range []string{"deficit", "proportional", "invalid", ""} {
		aggregate := validEntryAggregate()
		aggregate.Data.ILLConfig = &ILLConfig{LoadBalancingPolicy: &policy}
		err := aggregate.NormalizeAndValidate()
		if policy == "invalid" || policy == "" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}
