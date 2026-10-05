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
	return EntryAggregate{Key: uuid.New(), Data: EntryData{Name: "Library", Type: "Institution", Symbols: []SymbolRef{{Authority: "isil", Symbol: "lib"}}}}
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
