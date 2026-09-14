package importdb

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestRunImportEntryAttemptsRetriesTransactionConflicts(t *testing.T) {
	attempts := 0
	want := model.RepoResult{Outcome: model.OutcomeImported}

	result, err := runImportEntryAttempts(context.Background(), "ISIL:TEST", func() (model.RepoResult, error) {
		attempts++
		switch attempts {
		case 1:
			return model.RepoResult{}, &pgconn.PgError{Code: "40P01"}
		case 2:
			return model.RepoResult{}, fmt.Errorf("commit: %w", &pgconn.PgError{Code: "40001"})
		default:
			return want, nil
		}
	})

	require.NoError(t, err)
	require.Equal(t, want, result)
	require.Equal(t, 3, attempts)
}

func TestRunImportEntryAttemptsStopsForNonRetryableError(t *testing.T) {
	attempts := 0
	wantErr := errors.New("invalid entry")

	_, err := runImportEntryAttempts(context.Background(), "ISIL:TEST", func() (model.RepoResult, error) {
		attempts++
		return model.RepoResult{}, wantErr
	})

	require.ErrorIs(t, err, wantErr)
	require.Equal(t, 1, attempts)
}

func TestRunImportEntryAttemptsStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	_, err := runImportEntryAttempts(ctx, "ISIL:TEST", func() (model.RepoResult, error) {
		attempts++
		cancel()
		return model.RepoResult{}, &pgconn.PgError{Code: "40P01"}
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}

func TestRunImportEntryAttemptsDoesNotStartWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0

	_, err := runImportEntryAttempts(ctx, "ISIL:TEST", func() (model.RepoResult, error) {
		attempts++
		return model.RepoResult{Outcome: model.OutcomeImported}, nil
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, attempts)
}

func TestRetryableImportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "entry mapping changed", err: errImportEntryMappingChanged, want: true},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: true},
		{name: "wrapped serialization failure", err: fmt.Errorf("lock entry hierarchy: %w", &pgconn.PgError{Code: "40001"}), want: true},
		{name: "non-retryable PostgreSQL error", err: &pgconn.PgError{Code: "23505"}, want: false},
		{name: "ordinary error", err: errors.New("failed"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, retryableImportError(test.err))
		})
	}
}

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
