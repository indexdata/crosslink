package importdb

import (
	"context"
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	brokerepo "github.com/indexdata/crosslink/broker/repo"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateImportRepoInitializesDependencies(t *testing.T) {
	repo := CreateImportRepo(nil)
	pgRepo, ok := repo.(*PgImportRepo)
	require.True(t, ok)

	assert.Nil(t, pgRepo.Pool)
	assert.NotNil(t, pgRepo.queries)
	assert.NotNil(t, pgRepo.prQueries)
	assert.NotNil(t, pgRepo.illQueries)
	assert.NotNil(t, pgRepo.schedQueries)
}

func TestCreateWithPgBaseRepoCopiesBaseAndQueries(t *testing.T) {
	source := &PgImportRepo{
		queries:      New(),
		prQueries:    pr_db.New(),
		illQueries:   ill_db.New(),
		schedQueries: sched_db.New(),
	}
	base := &brokerepo.PgBaseRepo[ImportRepo]{}

	derivedRepo := source.CreateWithPgBaseRepo(base)
	derived, ok := derivedRepo.(*PgImportRepo)
	require.True(t, ok)

	assert.Equal(t, *base, derived.PgBaseRepo)
	assert.Same(t, source.queries, derived.queries)
	assert.Same(t, source.prQueries, derived.prQueries)
	assert.Same(t, source.illQueries, derived.illQueries)
	assert.Same(t, source.schedQueries, derived.schedQueries)
}

func TestEnsureImportParent(t *testing.T) {
	t.Run("missing row is allowed", func(t *testing.T) {
		err := ensureImportParent(context.Background(), "id-1", "pr-1", "item", func(string) (string, error) {
			return "", pgx.ErrNoRows
		})
		require.NoError(t, err)
	})

	t.Run("query failure is wrapped", func(t *testing.T) {
		dbErr := errors.New("db down")
		err := ensureImportParent(context.Background(), "id-1", "pr-1", "item", func(string) (string, error) {
			return "", dbErr
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, dbErr)
		assert.Contains(t, err.Error(), "check item \"id-1\" ownership")
	})

	t.Run("mismatched parent returns conflict", func(t *testing.T) {
		err := ensureImportParent(context.Background(), "id-1", "pr-1", "item", func(string) (string, error) {
			return "pr-2", nil
		})
		var conflictErr *ConflictError
		require.ErrorAs(t, err, &conflictErr)
		assert.Equal(t, "item", conflictErr.Resource)
		assert.Equal(t, "id-1", conflictErr.Identifier)
		assert.Contains(t, conflictErr.Reason, "belongs to \"pr-2\"")
	})

	t.Run("matching parent succeeds", func(t *testing.T) {
		err := ensureImportParent(context.Background(), "id-1", "pr-1", "item", func(string) (string, error) {
			return "pr-1", nil
		})
		require.NoError(t, err)
	})
}
