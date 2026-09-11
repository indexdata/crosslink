package importdb

import (
	"testing"

	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	brokerepo "github.com/indexdata/crosslink/broker/repo"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
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
