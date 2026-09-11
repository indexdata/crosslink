package importdb

import (
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/stretchr/testify/require"
)

func TestOrderedUniqueEntryIDsSortsAndDeduplicates(t *testing.T) {
	first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	require.Equal(t, []uuid.UUID{first, second}, orderedUniqueEntryIDs(second, first, second))
}

func TestEntryLockIDsIncludesOwnerParentAndLenders(t *testing.T) {
	ownerID := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	parentID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	firstLenderID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	secondLenderID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	owner := db.Entry{ID: ownerID}
	parent := db.Entry{ID: parentID}
	lenders := []db.Entry{{ID: firstLenderID}, {ID: secondLenderID}, {ID: firstLenderID}}

	require.Equal(t,
		[]uuid.UUID{secondLenderID, firstLenderID, parentID, ownerID},
		entryLockIDs(&owner, &parent, lenders),
	)
}
