package importdb_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/app"
	importdb "github.com/indexdata/crosslink/directory/import/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("directory_import_test"),
		postgres.WithUsername("directory"),
		postgres.WithPassword("directory"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(10*time.Second)),
	)
	if err != nil {
		panic(fmt.Sprintf("start postgres: %v", err))
	}
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("get postgres connection string: %v", err))
	}
	app.ConnectionString = connectionString
	app.MigrationsFolder = "file://../../migrations"
	app.RunMigrateScripts()
	testPool = app.InitDbPool()

	code := m.Run()
	testPool.Close()
	if err := container.Terminate(ctx); err != nil {
		panic(fmt.Sprintf("terminate postgres: %v", err))
	}
	os.Exit(code)
}

func TestImportBusinessKeyConstraints(t *testing.T) {
	ctx := context.Background()
	consortiumID := uuid.New()
	_, err := testPool.Exec(ctx, `INSERT INTO entries (id, name, type) VALUES ($1, 'Consortium', 'Consortium')`, consortiumID)
	require.NoError(t, err)

	_, err = testPool.Exec(ctx, `
		INSERT INTO tiers (consortium, name, level, type, cost)
		VALUES ($1, 'Loan', 'standard', 'loan', 0), ($1, 'Loan', 'standard', 'loan', 0)`, consortiumID)
	requirePgCode(t, err, "23505")

	_, err = testPool.Exec(ctx, `
		INSERT INTO networks (consortium, name)
		VALUES ($1, 'Main'), ($1, 'Main')`, consortiumID)
	requirePgCode(t, err, "23505")

	_, err = testPool.Exec(ctx, `INSERT INTO tiers (consortium, name, level, type, cost) VALUES ($1, NULL, 'standard', 'loan', 0)`, consortiumID)
	requirePgCode(t, err, "23502")

	_, err = testPool.Exec(ctx, `INSERT INTO networks (consortium, name) VALUES ($1, NULL)`, consortiumID)
	requirePgCode(t, err, "23502")

	_, err = testPool.Exec(ctx, `INSERT INTO tiers (consortium, name, level, type, cost) VALUES ($1, E'\t\n', 'standard', 'loan', 0)`, consortiumID)
	requirePgCode(t, err, "23514")

	_, err = testPool.Exec(ctx, `INSERT INTO networks (consortium, name) VALUES ($1, E'\t\n')`, consortiumID)
	requirePgCode(t, err, "23514")
}

func TestImportEntryCreatesCompleteAggregateWithGeneratedIDs(t *testing.T) {
	resetImportDatabase(t)
	repo := importdb.New(testPool)
	aggregate := completeEntryAggregate("CON")

	result, err := repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyFail)

	require.NoError(t, err)
	require.Equal(t, model.OutcomeImported, result.Outcome)
	entryID := entryIDBySymbol(t, aggregate.Key)
	require.NotEqual(t, uuid.Nil, entryID)
	for table, expected := range map[string]int{
		"symbols": 1, "service_endpoints": 1, "addresses": 1, "address_components": 1, "closures": 1,
		"lms_configs": 1, "catalog_configs": 1, "ill_configs": 1, "holdings_policies": 1,
	} {
		assertOwnedCount(t, table, entryID, expected)
	}
	var authentication string
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT from_agency_authentication FROM lms_configs WHERE entry=$1`, entryID).Scan(&authentication))
	require.Equal(t, "credential-value", authentication)
	var zoomOptions map[string]string
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT zoom_options FROM catalog_configs WHERE entry=$1`, entryID).Scan(&zoomOptions))
	require.Equal(t, map[string]string{"user": "private"}, zoomOptions)
}

func TestImportEntryConflictPoliciesAndUpdateFullSynchronization(t *testing.T) {
	resetImportDatabase(t)
	repo := importdb.New(testPool)
	aggregate := completeEntryAggregate("CON")
	_, err := repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyFail)
	require.NoError(t, err)
	entryID := entryIDBySymbol(t, aggregate.Key)
	var originalEndpointID uuid.UUID
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT id FROM service_endpoints WHERE entry=$1`, entryID).Scan(&originalEndpointID))

	_, err = repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyFail)
	require.ErrorContains(t, err, "already exists")
	skipped, err := repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicySkip)
	require.NoError(t, err)
	require.Equal(t, model.OutcomeSkipped, skipped.Outcome)

	aggregate.Data.Name = "Updated Consortium"
	aggregate.Data.Endpoints = []model.ServiceEndpoint{}
	aggregate.Data.LMSConfig = nil
	aggregate.Data.CatalogConfig = nil
	aggregate.Data.ILLConfig = nil
	aggregate.Data.HoldingsPolicy = nil
	updated, err := repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyUpdate)
	require.NoError(t, err)
	require.Equal(t, model.OutcomeImported, updated.Outcome)
	require.Equal(t, entryID, entryIDBySymbol(t, aggregate.Key))
	assertOwnedCount(t, "service_endpoints", entryID, 0)
	assertOwnedCount(t, "lms_configs", entryID, 0)
	assertOwnedCount(t, "catalog_configs", entryID, 0)
	assertOwnedCount(t, "ill_configs", entryID, 0)
	assertOwnedCount(t, "holdings_policies", entryID, 0)
	var name string
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT name FROM entries WHERE id=$1`, entryID).Scan(&name))
	require.Equal(t, "Updated Consortium", name)

	aggregate.Data.Endpoints = []model.ServiceEndpoint{{Name: "Replacement", Type: "ISO18626", Address: "https://replacement.test"}}
	_, err = repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyUpdate)
	require.NoError(t, err)
	var replacementEndpointID uuid.UUID
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT id FROM service_endpoints WHERE entry=$1`, entryID).Scan(&replacementEndpointID))
	require.NotEqual(t, originalEndpointID, replacementEndpointID)
}

func TestImportEntryUpdateReplacesLMSPatronProfiles(t *testing.T) {
	resetImportDatabase(t)
	repo := importdb.New(testPool)
	aggregate := completeEntryAggregate("CON")
	initialProfiles := []model.PatronProfile{{Code: stringPointer("STAFF"), CanCreateRequests: true}}
	aggregate.Data.LMSConfig.PatronProfiles = &initialProfiles

	_, err := repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyFail)
	require.NoError(t, err)
	entryID := entryIDBySymbol(t, aggregate.Key)
	require.JSONEq(t, `[{"code":"STAFF","canCreateRequests":true}]`, lmsPatronProfiles(t, entryID))

	replacementProfiles := []model.PatronProfile{{Name: stringPointer("Blocked"), CanCreateRequests: false}}
	aggregate.Data.LMSConfig.PatronProfiles = &replacementProfiles
	_, err = repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyUpdate)
	require.NoError(t, err)
	require.JSONEq(t, `[{"name":"Blocked","canCreateRequests":false}]`, lmsPatronProfiles(t, entryID))
}

func TestConcurrentImportEntrySkipHonorsConflictPolicyForMissingKey(t *testing.T) {
	resetImportDatabase(t)
	newAggregate := func() model.EntryAggregate { return minimalEntryAggregate("concurrent", "Institution") }
	results, errs := concurrentlyImportEntry(t, newAggregate, model.ConflictPolicySkip, 8)

	var imported, skipped int
	for index, err := range errs {
		require.NoError(t, err)
		switch results[index].Outcome {
		case model.OutcomeImported:
			imported++
		case model.OutcomeSkipped:
			skipped++
		}
	}
	require.Equal(t, 1, imported)
	require.Equal(t, 7, skipped)
	require.Equal(t, 1, entryCount(t))
}

func TestConcurrentImportEntryUpdateHonorsConflictPolicyForMissingKey(t *testing.T) {
	resetImportDatabase(t)
	newAggregate := func() model.EntryAggregate { return minimalEntryAggregate("concurrent", "Institution") }
	results, errs := concurrentlyImportEntry(t, newAggregate, model.ConflictPolicyUpdate, 8)

	for index, err := range errs {
		require.NoError(t, err)
		require.Equal(t, model.OutcomeImported, results[index].Outcome)
	}
	require.Equal(t, 1, entryCount(t))
}

func TestConcurrentImportEntryOpposingParentsDoNotDeadlock(t *testing.T) {
	resetImportDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const pairCount = 12
	type importPair struct {
		first  model.EntryAggregate
		second model.EntryAggregate
	}
	pairs := make([]importPair, pairCount)
	for index := range pairCount {
		firstID, secondID := uuid.New(), uuid.New()
		firstSymbol := fmt.Sprintf("A-%d", index)
		secondSymbol := fmt.Sprintf("B-%d", index)
		_, err := testPool.Exec(ctx, `INSERT INTO entries (id, name, type) VALUES ($1, $2, 'Institution'), ($3, $4, 'Institution')`,
			firstID, "Institution "+firstSymbol, secondID, "Institution "+secondSymbol)
		require.NoError(t, err)
		_, err = testPool.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'ISIL', $2), ($3, 'ISIL', $4)`,
			firstID, firstSymbol, secondID, secondSymbol)
		require.NoError(t, err)

		first := minimalEntryAggregate(firstSymbol, "Branch")
		first.Data.Parent = &model.SymbolRef{Authority: "ISIL", Symbol: secondSymbol}
		second := minimalEntryAggregate(secondSymbol, "Branch")
		second.Data.Parent = &model.SymbolRef{Authority: "ISIL", Symbol: firstSymbol}
		pairs[index] = importPair{first: first, second: second}
	}

	repo := importdb.New(testPool)
	start := make(chan struct{})
	errs := make([][2]error, pairCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(pairCount * 2)
	for index := range pairs {
		go func() {
			defer waitGroup.Done()
			<-start
			_, errs[index][0] = repo.ImportEntry(ctx, pairs[index].first, model.ConflictPolicyUpdate)
		}()
		go func() {
			defer waitGroup.Done()
			<-start
			_, errs[index][1] = repo.ImportEntry(ctx, pairs[index].second, model.ConflictPolicyUpdate)
		}()
	}
	close(start)
	waitGroup.Wait()
	require.NoError(t, ctx.Err())

	for index, pairErrors := range errs {
		var imported int
		for _, err := range pairErrors {
			if err == nil {
				imported++
				continue
			}
			var pgErr *pgconn.PgError
			require.Falsef(t, errors.As(err, &pgErr) && pgErr.Code == "40P01", "pair %d deadlocked: %v", index, err)
			require.Truef(t,
				strings.Contains(err.Error(), "invalid parent") || strings.Contains(err.Error(), "would create a cycle"),
				"pair %d returned an unexpected error: %v", index, err)
		}
		require.Equalf(t, 1, imported, "pair %d should serialize before hierarchy validation", index)
	}
}

func TestConcurrentImportEntryOpposingLendersDoNotDeadlock(t *testing.T) {
	resetImportDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const pairCount = 12
	type importPair struct {
		first  model.EntryAggregate
		second model.EntryAggregate
	}
	pairs := make([]importPair, pairCount)
	for index := range pairCount {
		firstID, secondID := uuid.New(), uuid.New()
		firstSymbol := fmt.Sprintf("LENDER-A-%d", index)
		secondSymbol := fmt.Sprintf("LENDER-B-%d", index)
		_, err := testPool.Exec(ctx, `INSERT INTO entries (id, name, type) VALUES ($1, $2, 'Institution'), ($3, $4, 'Institution')`,
			firstID, "Institution "+firstSymbol, secondID, "Institution "+secondSymbol)
		require.NoError(t, err)
		_, err = testPool.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'ISIL', $2), ($3, 'ISIL', $4)`,
			firstID, firstSymbol, secondID, secondSymbol)
		require.NoError(t, err)

		first := minimalEntryAggregate(firstSymbol, "Institution")
		first.Data.ILLConfig = &model.ILLConfig{LendersOfLastResort: []model.SymbolRef{{Authority: "ISIL", Symbol: secondSymbol}}}
		second := minimalEntryAggregate(secondSymbol, "Institution")
		second.Data.ILLConfig = &model.ILLConfig{LendersOfLastResort: []model.SymbolRef{{Authority: "ISIL", Symbol: firstSymbol}}}
		pairs[index] = importPair{first: first, second: second}
	}

	repo := importdb.New(testPool)
	start := make(chan struct{})
	errs := make([][2]error, pairCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(pairCount * 2)
	for index := range pairs {
		go func() {
			defer waitGroup.Done()
			<-start
			_, errs[index][0] = repo.ImportEntry(ctx, pairs[index].first, model.ConflictPolicyUpdate)
		}()
		go func() {
			defer waitGroup.Done()
			<-start
			_, errs[index][1] = repo.ImportEntry(ctx, pairs[index].second, model.ConflictPolicyUpdate)
		}()
	}
	close(start)
	waitGroup.Wait()
	require.NoError(t, ctx.Err())

	for index, pairErrors := range errs {
		require.NoErrorf(t, pairErrors[0], "first import in pair %d failed", index)
		require.NoErrorf(t, pairErrors[1], "second import in pair %d failed", index)
	}
}

func TestImportEntryRetriesWhenParentSymbolChangesBeforeRowLock(t *testing.T) {
	resetImportDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	originalParentID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	ownerID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	replacementParentID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	_, err := testPool.Exec(ctx, `
		INSERT INTO entries (id, name, type) VALUES
			($1, 'Original parent', 'Institution'),
			($2, 'Imported branch', 'Branch'),
			($3, 'Replacement parent', 'Institution')`, originalParentID, ownerID, replacementParentID)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `
		INSERT INTO symbols (owner, authority, symbol) VALUES
			($1, 'ISIL', 'PARENT'),
			($2, 'ISIL', 'BRANCH')`, originalParentID, ownerID)
	require.NoError(t, err)

	blocker, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()
	_, err = blocker.Exec(ctx, `SELECT id FROM entries WHERE id=$1 FOR UPDATE`, originalParentID)
	require.NoError(t, err)

	aggregate := minimalEntryAggregate("BRANCH", "Branch")
	aggregate.Data.Parent = &model.SymbolRef{Authority: "ISIL", Symbol: "PARENT"}
	importDone := make(chan error, 1)
	go func() {
		_, importErr := importdb.New(testPool).ImportEntry(ctx, aggregate, model.ConflictPolicyUpdate)
		importDone <- importErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := testPool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND pid <> pg_backend_pid() AND wait_event_type='Lock'
		)`).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)

	_, err = testPool.Exec(ctx, `UPDATE symbols SET owner=$1 WHERE authority='ISIL' AND symbol='PARENT'`, replacementParentID)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-importDone)

	var parentID uuid.UUID
	require.NoError(t, testPool.QueryRow(ctx, `SELECT parent FROM entries WHERE id=$1`, ownerID).Scan(&parentID))
	require.Equal(t, replacementParentID, parentID)
}

func TestImportEntryRetriesWhenParentIsDeletedBeforeRowLock(t *testing.T) {
	resetImportDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parentID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	ownerID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	_, err := testPool.Exec(ctx, `INSERT INTO entries (id, name, type) VALUES ($1, 'Parent', 'Institution'), ($2, 'Branch', 'Branch')`, parentID, ownerID)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'ISIL', 'PARENT'), ($2, 'ISIL', 'BRANCH')`, parentID, ownerID)
	require.NoError(t, err)

	blocker, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()
	_, err = blocker.Exec(ctx, `SELECT id FROM entries WHERE id=$1 FOR UPDATE`, parentID)
	require.NoError(t, err)

	aggregate := minimalEntryAggregate("BRANCH", "Branch")
	aggregate.Data.Parent = &model.SymbolRef{Authority: "ISIL", Symbol: "PARENT"}
	importDone := make(chan error, 1)
	go func() {
		_, importErr := importdb.New(testPool).ImportEntry(ctx, aggregate, model.ConflictPolicyUpdate)
		importDone <- importErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := testPool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND pid <> pg_backend_pid() AND wait_event_type='Lock'
		)`).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)

	_, err = blocker.Exec(ctx, `DELETE FROM entries WHERE id=$1`, parentID)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit(ctx))
	require.ErrorContains(t, <-importDone, "parent ISIL:PARENT does not exist")
}

func TestImportEntryRejectsInvalidHierarchy(t *testing.T) {
	resetImportDatabase(t)
	repo := importdb.New(testPool)
	consortium := minimalEntryAggregate("CON", "Consortium")
	_, err := repo.ImportEntry(context.Background(), consortium, model.ConflictPolicyFail)
	require.NoError(t, err)

	branch := minimalEntryAggregate("BRANCH", "Branch")
	branch.Data.Parent = &consortium.Key
	_, err = repo.ImportEntry(context.Background(), branch, model.ConflictPolicyFail)

	require.ErrorContains(t, err, "Branch parent must be of type Institution")
	assertEntryDoesNotExist(t, branch.Key)
}

func TestImportEntryRejectsCycle(t *testing.T) {
	resetImportDatabase(t)
	ctx := context.Background()
	repo := importdb.New(testPool)
	branchID, institutionID := uuid.New(), uuid.New()
	_, err := testPool.Exec(ctx, `INSERT INTO entries (id,name,type,parent) VALUES ($1,'Branch','Branch',NULL),($2,'Institution','Institution',$1)`, branchID, institutionID)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `INSERT INTO symbols (owner,authority,symbol) VALUES ($1,'ISIL','BRANCH'),($2,'ISIL','INST')`, branchID, institutionID)
	require.NoError(t, err)

	branch := minimalEntryAggregate("BRANCH", "Branch")
	branch.Data.Parent = &model.SymbolRef{Authority: "ISIL", Symbol: "INST"}
	_, err = repo.ImportEntry(ctx, branch, model.ConflictPolicyUpdate)

	require.ErrorContains(t, err, "would create a cycle")
}

func TestImportEntryRejectsMissingParentWithoutWriting(t *testing.T) {
	resetImportDatabase(t)
	repo := importdb.New(testPool)
	aggregate := minimalEntryAggregate("LIB", "Institution")
	aggregate.Data.Parent = &model.SymbolRef{Authority: "ISIL", Symbol: "MISSING"}

	_, err := repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyFail)

	require.ErrorContains(t, err, "parent ISIL:MISSING does not exist")
	assertEntryDoesNotExist(t, aggregate.Key)
}

func TestImportEntryRollsBackAfterLateSymbolConflict(t *testing.T) {
	resetImportDatabase(t)
	repo := importdb.New(testPool)
	consortium := completeEntryAggregate("CON")
	_, err := repo.ImportEntry(context.Background(), consortium, model.ConflictPolicyFail)
	require.NoError(t, err)

	aggregate := minimalEntryAggregate("LIB", "Institution")
	aggregate.Data.Parent = &consortium.Key
	aggregate.Data.Symbols = append(aggregate.Data.Symbols, consortium.Key)
	_, err = repo.ImportEntry(context.Background(), aggregate, model.ConflictPolicyFail)

	require.Error(t, err)
	assertEntryDoesNotExist(t, aggregate.Key)
}

func TestImportEntryAllowsOnlyOneConsortium(t *testing.T) {
	resetImportDatabase(t)
	repo := importdb.New(testPool)
	_, err := repo.ImportEntry(context.Background(), minimalEntryAggregate("CON1", "Consortium"), model.ConflictPolicyFail)
	require.NoError(t, err)

	second := minimalEntryAggregate("CON2", "Consortium")
	_, err = repo.ImportEntry(context.Background(), second, model.ConflictPolicyFail)

	require.ErrorContains(t, err, "consortium already exists")
	assertEntryDoesNotExist(t, second.Key)
}

func TestImportTierConflictPoliciesAndUpdateReplacesAssignments(t *testing.T) {
	repo, consortium, first, second := importRepoFixture(t)
	aggregate := model.TierAggregate{
		Key:  model.TierKey{Consortium: consortium, Name: "Loan"},
		Data: model.TierData{Level: "standard", Type: "loan", Cost: 1.5, Entries: []model.SymbolRef{first}},
	}

	result, err := repo.ImportTier(context.Background(), aggregate, model.ConflictPolicyFail)
	require.NoError(t, err)
	require.Equal(t, model.OutcomeImported, result.Outcome)
	id := tierIDByKey(t, consortium, "Loan")
	require.NotEqual(t, uuid.Nil, id)
	require.Equal(t, []model.SymbolRef{first}, tierAssignments(t, id))

	_, err = repo.ImportTier(context.Background(), aggregate, model.ConflictPolicyFail)
	require.ErrorContains(t, err, "already exists")
	skipped, err := repo.ImportTier(context.Background(), aggregate, model.ConflictPolicySkip)
	require.NoError(t, err)
	require.Equal(t, model.OutcomeSkipped, skipped.Outcome)

	aggregate.Data.Level = "rush"
	aggregate.Data.Cost = 2.5
	aggregate.Data.Entries = []model.SymbolRef{second}
	_, err = repo.ImportTier(context.Background(), aggregate, model.ConflictPolicyUpdate)
	require.NoError(t, err)
	require.Equal(t, id, tierIDByKey(t, consortium, "Loan"))
	require.Equal(t, []model.SymbolRef{second}, tierAssignments(t, id))
}

func TestConcurrentEntryAndTierImportsUseSameEntryLockOrder(t *testing.T) {
	resetImportDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	memberID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	consortiumID := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	tierID := uuid.New()
	_, err := testPool.Exec(ctx, `INSERT INTO entries (id, name, type) VALUES ($1, 'Member', 'Institution'), ($2, 'Consortium', 'Consortium')`, memberID, consortiumID)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'ISIL', 'MEMBER'), ($2, 'ISIL', 'CON')`, memberID, consortiumID)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `INSERT INTO tiers (id, consortium, name, level, type, cost) VALUES ($1, $2, 'Loan', 'standard', 'loan', 0)`, tierID, consortiumID)
	require.NoError(t, err)

	blocker, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()
	_, err = blocker.Exec(ctx, `SELECT id FROM tiers WHERE id=$1 FOR UPDATE`, tierID)
	require.NoError(t, err)

	repo := importdb.New(testPool)
	consortium := model.SymbolRef{Authority: "ISIL", Symbol: "CON"}
	member := model.SymbolRef{Authority: "ISIL", Symbol: "MEMBER"}
	tier := model.TierAggregate{
		Key:  model.TierKey{Consortium: consortium, Name: "Loan"},
		Data: model.TierData{Level: "standard", Type: "loan", Entries: []model.SymbolRef{member}},
	}
	tierDone := make(chan error, 1)
	go func() {
		_, importErr := repo.ImportTier(ctx, tier, model.ConflictPolicyUpdate)
		tierDone <- importErr
	}()
	waitForDatabaseLockWaiters(t, ctx, 1)

	entry := minimalEntryAggregate("MEMBER", "Institution")
	entry.Data.Parent = &consortium
	entryDone := make(chan error, 1)
	go func() {
		_, importErr := repo.ImportEntry(ctx, entry, model.ConflictPolicyUpdate)
		entryDone <- importErr
	}()
	waitForDatabaseLockWaiters(t, ctx, 2)

	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-tierDone)
	require.NoError(t, <-entryDone)
}

func TestConcurrentImportTierSkipHonorsConflictPolicyForMissingKey(t *testing.T) {
	repo, consortium, _, _ := importRepoFixture(t)
	newAggregate := func() model.TierAggregate {
		return model.TierAggregate{
			Key:  model.TierKey{Consortium: consortium, Name: "Concurrent"},
			Data: model.TierData{Level: "standard", Type: "loan", Entries: []model.SymbolRef{}},
		}
	}
	results, errs := concurrentlyImportTier(repo, newAggregate, model.ConflictPolicySkip, 8)

	requireImportOutcomes(t, results, errs, 1, 7)
	require.Equal(t, 1, aggregateCount(t, "tiers"))
}

func TestConcurrentImportTierUpdateHonorsConflictPolicyForMissingKey(t *testing.T) {
	repo, consortium, _, _ := importRepoFixture(t)
	newAggregate := func() model.TierAggregate {
		return model.TierAggregate{
			Key:  model.TierKey{Consortium: consortium, Name: "Concurrent"},
			Data: model.TierData{Level: "standard", Type: "loan", Entries: []model.SymbolRef{}},
		}
	}
	results, errs := concurrentlyImportTier(repo, newAggregate, model.ConflictPolicyUpdate, 8)

	requireImportOutcomes(t, results, errs, 8, 0)
	require.Equal(t, 1, aggregateCount(t, "tiers"))
}

func TestImportTierRollsBackWhenMemberIsMissing(t *testing.T) {
	repo, consortium, first, _ := importRepoFixture(t)
	missing := model.SymbolRef{Authority: "ISIL", Symbol: "MISSING"}
	aggregate := model.TierAggregate{
		Key:  model.TierKey{Consortium: consortium, Name: "Loan"},
		Data: model.TierData{Level: "standard", Type: "loan", Entries: []model.SymbolRef{first, missing}},
	}

	_, err := repo.ImportTier(context.Background(), aggregate, model.ConflictPolicyFail)

	require.ErrorContains(t, err, "entry ISIL:MISSING does not exist")
	assertTierDoesNotExist(t, consortium, "Loan")
}

func TestImportNetworkConflictPoliciesAndUpdateReplacesAssignments(t *testing.T) {
	repo, consortium, first, second := importRepoFixture(t)
	reciprocal := true
	aggregate := model.NetworkAggregate{
		Key:  model.NetworkKey{Consortium: consortium, Name: "Main"},
		Data: model.NetworkData{Reciprocal: &reciprocal, Entries: []model.NetworkAssignment{{SymbolRef: first, Priority: 1}}},
	}

	result, err := repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicyFail)
	require.NoError(t, err)
	require.Equal(t, model.OutcomeImported, result.Outcome)
	id := networkIDByKey(t, consortium, "Main")
	require.NotEqual(t, uuid.Nil, id)
	require.Equal(t, []model.NetworkAssignment{{SymbolRef: first, Priority: 1}}, networkAssignments(t, id))

	_, err = repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicyFail)
	require.ErrorContains(t, err, "already exists")
	skipped, err := repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicySkip)
	require.NoError(t, err)
	require.Equal(t, model.OutcomeSkipped, skipped.Outcome)

	aggregate.Data.Reciprocal = nil
	aggregate.Data.Entries = []model.NetworkAssignment{{SymbolRef: second, Priority: 2}}
	_, err = repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicyUpdate)
	require.NoError(t, err)
	require.Equal(t, id, networkIDByKey(t, consortium, "Main"))
	require.Equal(t, []model.NetworkAssignment{{SymbolRef: second, Priority: 2}}, networkAssignments(t, id))
}

func TestConcurrentEntryAndNetworkImportsUseSameEntryLockOrder(t *testing.T) {
	resetImportDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	memberID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	consortiumID := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	networkID := uuid.New()
	_, err := testPool.Exec(ctx, `INSERT INTO entries (id, name, type) VALUES ($1, 'Member', 'Institution'), ($2, 'Consortium', 'Consortium')`, memberID, consortiumID)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'ISIL', 'MEMBER'), ($2, 'ISIL', 'CON')`, memberID, consortiumID)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `INSERT INTO networks (id, consortium, name) VALUES ($1, $2, 'Main')`, networkID, consortiumID)
	require.NoError(t, err)

	blocker, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()
	_, err = blocker.Exec(ctx, `SELECT id FROM networks WHERE id=$1 FOR UPDATE`, networkID)
	require.NoError(t, err)

	repo := importdb.New(testPool)
	consortium := model.SymbolRef{Authority: "ISIL", Symbol: "CON"}
	member := model.SymbolRef{Authority: "ISIL", Symbol: "MEMBER"}
	network := model.NetworkAggregate{
		Key:  model.NetworkKey{Consortium: consortium, Name: "Main"},
		Data: model.NetworkData{Entries: []model.NetworkAssignment{{SymbolRef: member, Priority: 1}}},
	}
	networkDone := make(chan error, 1)
	go func() {
		_, importErr := repo.ImportNetwork(ctx, network, model.ConflictPolicyUpdate)
		networkDone <- importErr
	}()
	waitForDatabaseLockWaiters(t, ctx, 1)

	entry := minimalEntryAggregate("MEMBER", "Institution")
	entry.Data.Parent = &consortium
	entryDone := make(chan error, 1)
	go func() {
		_, importErr := repo.ImportEntry(ctx, entry, model.ConflictPolicyUpdate)
		entryDone <- importErr
	}()
	waitForDatabaseLockWaiters(t, ctx, 2)

	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-networkDone)
	require.NoError(t, <-entryDone)
}

func TestConcurrentImportNetworkSkipHonorsConflictPolicyForMissingKey(t *testing.T) {
	repo, consortium, _, _ := importRepoFixture(t)
	newAggregate := func() model.NetworkAggregate {
		return model.NetworkAggregate{
			Key:  model.NetworkKey{Consortium: consortium, Name: "Concurrent"},
			Data: model.NetworkData{Entries: []model.NetworkAssignment{}},
		}
	}
	results, errs := concurrentlyImportNetwork(repo, newAggregate, model.ConflictPolicySkip, 8)

	requireImportOutcomes(t, results, errs, 1, 7)
	require.Equal(t, 1, aggregateCount(t, "networks"))
}

func TestConcurrentImportNetworkUpdateHonorsConflictPolicyForMissingKey(t *testing.T) {
	repo, consortium, _, _ := importRepoFixture(t)
	newAggregate := func() model.NetworkAggregate {
		return model.NetworkAggregate{
			Key:  model.NetworkKey{Consortium: consortium, Name: "Concurrent"},
			Data: model.NetworkData{Entries: []model.NetworkAssignment{}},
		}
	}
	results, errs := concurrentlyImportNetwork(repo, newAggregate, model.ConflictPolicyUpdate, 8)

	requireImportOutcomes(t, results, errs, 8, 0)
	require.Equal(t, 1, aggregateCount(t, "networks"))
}

func TestImportNetworkRejectsNonConsortiumOwner(t *testing.T) {
	repo, _, first, _ := importRepoFixture(t)
	aggregate := model.NetworkAggregate{
		Key:  model.NetworkKey{Consortium: first, Name: "Main"},
		Data: model.NetworkData{Entries: []model.NetworkAssignment{}},
	}

	_, err := repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicyFail)

	require.ErrorContains(t, err, "is not a consortium")
}

func importRepoFixture(t *testing.T) (*importdb.PgImportRepo, model.SymbolRef, model.SymbolRef, model.SymbolRef) {
	t.Helper()
	resetImportDatabase(t)
	repo := importdb.New(testPool)
	consortium := minimalEntryAggregate("CON", "Consortium")
	_, err := repo.ImportEntry(context.Background(), consortium, model.ConflictPolicyFail)
	require.NoError(t, err)
	first := minimalEntryAggregate("FIRST", "Institution")
	first.Data.Parent = &consortium.Key
	_, err = repo.ImportEntry(context.Background(), first, model.ConflictPolicyFail)
	require.NoError(t, err)
	second := minimalEntryAggregate("SECOND", "Institution")
	second.Data.Parent = &consortium.Key
	_, err = repo.ImportEntry(context.Background(), second, model.ConflictPolicyFail)
	require.NoError(t, err)
	return repo, consortium.Key, first.Key, second.Key
}

func tierIDByKey(t *testing.T, consortium model.SymbolRef, name string) uuid.UUID {
	t.Helper()
	return aggregateIDByKey(t, "tiers", consortium, name)
}

func networkIDByKey(t *testing.T, consortium model.SymbolRef, name string) uuid.UUID {
	t.Helper()
	return aggregateIDByKey(t, "networks", consortium, name)
}

func aggregateIDByKey(t *testing.T, table string, consortium model.SymbolRef, name string) uuid.UUID {
	t.Helper()
	query := fmt.Sprintf(`SELECT a.id FROM %s a JOIN symbols s ON s.owner=a.consortium WHERE s.authority=$1 AND s.symbol=$2 AND a.name=$3`, table) //nolint:gosec // table names are fixed test constants
	var id uuid.UUID
	require.NoError(t, testPool.QueryRow(context.Background(), query, consortium.Authority, consortium.Symbol, name).Scan(&id))
	return id
}

func tierAssignments(t *testing.T, id uuid.UUID) []model.SymbolRef {
	t.Helper()
	return aggregateAssignments(t, "entry_tiers", "tier", id)
}

func networkAssignments(t *testing.T, id uuid.UUID) []model.NetworkAssignment {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT s.authority, s.symbol, en.priority
		FROM entry_networks en
		JOIN symbols s ON s.owner=en.entry
		WHERE en.network=$1
		ORDER BY s.authority, s.symbol`, id)
	require.NoError(t, err)
	defer rows.Close()
	var result []model.NetworkAssignment
	for rows.Next() {
		var assignment model.NetworkAssignment
		require.NoError(t, rows.Scan(&assignment.Authority, &assignment.Symbol, &assignment.Priority))
		result = append(result, assignment)
	}
	require.NoError(t, rows.Err())
	return result
}

func aggregateAssignments(t *testing.T, table, aggregateColumn string, id uuid.UUID) []model.SymbolRef {
	t.Helper()
	query := fmt.Sprintf(`SELECT s.authority,s.symbol FROM %s a JOIN symbols s ON s.owner=a.entry WHERE a.%s=$1 ORDER BY s.authority,s.symbol`, table, aggregateColumn) //nolint:gosec // table and column names are fixed test constants
	rows, err := testPool.Query(context.Background(), query, id)
	require.NoError(t, err)
	defer rows.Close()
	var result []model.SymbolRef
	for rows.Next() {
		var ref model.SymbolRef
		require.NoError(t, rows.Scan(&ref.Authority, &ref.Symbol))
		result = append(result, ref)
	}
	require.NoError(t, rows.Err())
	return result
}

func assertTierDoesNotExist(t *testing.T, consortium model.SymbolRef, name string) {
	t.Helper()
	var count int
	err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM tiers t JOIN symbols s ON s.owner=t.consortium WHERE s.authority=$1 AND s.symbol=$2 AND t.name=$3`, consortium.Authority, consortium.Symbol, name).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
}

func lmsPatronProfiles(t *testing.T, entryID uuid.UUID) string {
	t.Helper()
	var profiles []byte
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT patron_profiles FROM lms_configs WHERE entry=$1`, entryID).Scan(&profiles))
	return string(profiles)
}

func completeEntryAggregate(symbol string) model.EntryAggregate {
	aggregate := minimalEntryAggregate(symbol, "Consortium")
	text := "value"
	metadataMode := "replace"
	truth := true
	aggregate.Data.Endpoints = []model.ServiceEndpoint{{Name: "ISO", Type: "ISO18626", Address: "https://example.test/ill"}}
	aggregate.Data.Addresses = []model.Address{{Type: "Default", Components: []model.AddressComponent{{Seq: 1, Type: "Locality", Value: "Riga"}}}}
	aggregate.Data.Closures = []model.Closure{{StartDate: "2026-12-24", EndDate: "2026-12-26", Reason: "Holiday"}}
	aggregate.Data.LMSConfig = &model.LMSConfig{Address: "https://example.test/ncip", FromAgency: "FROM", FromAgencyAuthentication: stringPointer("credential-value")}
	aggregate.Data.CatalogConfig = &model.CatalogConfig{
		MetadataUpdateMode: &metadataMode,
		SRU:                &model.SRUConfig{Address: "https://example.test/sru"},
		Zoom:               &model.ZoomConfig{Address: "example.test:210", Options: &map[string]string{"user": "private"}},
		Query:              &model.QueryConfig{Identifier: &text},
		HoldingsFormat:     &model.HoldingsParserConfig{Marc: &model.MarcHoldingsParserConfig{MainField: &text}},
		MetadataFormat:     &model.MetadataParserConfig{Marc21: &model.MarcMetadataParserConfig{Title: &text}},
	}
	aggregate.Data.ILLConfig = &model.ILLConfig{ISO18626URL: &text, LendersOfLastResort: []model.SymbolRef{}, IncludeSupplierInfo: &truth}
	aggregate.Data.HoldingsPolicy = &model.HoldingsPolicy{
		Locations:         []model.HoldingsLocation{{Code: "MAIN", Name: "Main", SupplyPreference: 1}},
		ShelvingLocations: []model.HoldingsShelvingLocation{},
		LocationPolicies:  []model.HoldingsLocationPolicy{},
		ItemLoanPolicies:  []model.HoldingsItemLoanPolicy{{Code: "BOOK", Name: "Book", Lendable: true}},
	}
	return aggregate
}

func stringPointer(value string) *string { return &value }

func minimalEntryAggregate(symbol, entryType string) model.EntryAggregate {
	key := model.SymbolRef{Authority: "ISIL", Symbol: symbol}
	return model.EntryAggregate{
		Key: key,
		Data: model.EntryData{
			Name: "Entry " + symbol, Type: entryType, Symbols: []model.SymbolRef{key}, Endpoints: []model.ServiceEndpoint{},
			Addresses: []model.Address{}, Closures: []model.Closure{},
		},
	}
}

func concurrentlyImportEntry(t *testing.T, newAggregate func() model.EntryAggregate, policy model.ConflictPolicy, count int) ([]model.RepoResult, []error) {
	t.Helper()
	repo := importdb.New(testPool)
	start := make(chan struct{})
	results := make([]model.RepoResult, count)
	errs := make([]error, count)
	var waitGroup sync.WaitGroup
	waitGroup.Add(count)
	for index := range count {
		go func() {
			defer waitGroup.Done()
			<-start
			results[index], errs[index] = repo.ImportEntry(context.Background(), newAggregate(), policy)
		}()
	}
	close(start)
	waitGroup.Wait()
	return results, errs
}

func waitForDatabaseLockWaiters(t *testing.T, ctx context.Context, minimum int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var count int
		err := testPool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND pid <> pg_backend_pid() AND wait_event_type='Lock'`).Scan(&count)
		return err == nil && count >= minimum
	}, 2*time.Second, 10*time.Millisecond)
}

func concurrentlyImportTier(repo *importdb.PgImportRepo, newAggregate func() model.TierAggregate, policy model.ConflictPolicy, count int) ([]model.RepoResult, []error) {
	start := make(chan struct{})
	results := make([]model.RepoResult, count)
	errs := make([]error, count)
	var waitGroup sync.WaitGroup
	waitGroup.Add(count)
	for index := range count {
		go func() {
			defer waitGroup.Done()
			<-start
			results[index], errs[index] = repo.ImportTier(context.Background(), newAggregate(), policy)
		}()
	}
	close(start)
	waitGroup.Wait()
	return results, errs
}

func concurrentlyImportNetwork(repo *importdb.PgImportRepo, newAggregate func() model.NetworkAggregate, policy model.ConflictPolicy, count int) ([]model.RepoResult, []error) {
	start := make(chan struct{})
	results := make([]model.RepoResult, count)
	errs := make([]error, count)
	var waitGroup sync.WaitGroup
	waitGroup.Add(count)
	for index := range count {
		go func() {
			defer waitGroup.Done()
			<-start
			results[index], errs[index] = repo.ImportNetwork(context.Background(), newAggregate(), policy)
		}()
	}
	close(start)
	waitGroup.Wait()
	return results, errs
}

func requireImportOutcomes(t *testing.T, results []model.RepoResult, errs []error, expectedImported, expectedSkipped int) {
	t.Helper()
	var imported, skipped int
	for index, err := range errs {
		require.NoError(t, err)
		switch results[index].Outcome {
		case model.OutcomeImported:
			imported++
		case model.OutcomeSkipped:
			skipped++
		}
	}
	require.Equal(t, expectedImported, imported)
	require.Equal(t, expectedSkipped, skipped)
}

func aggregateCount(t *testing.T, table string) int {
	t.Helper()
	query := fmt.Sprintf(`SELECT count(*) FROM %s`, table) //nolint:gosec // table names are fixed test constants
	var count int
	require.NoError(t, testPool.QueryRow(context.Background(), query).Scan(&count))
	return count
}

func entryCount(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT count(*) FROM entries`).Scan(&count))
	return count
}

func resetImportDatabase(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `TRUNCATE entries CASCADE`)
	require.NoError(t, err)
}

func entryIDBySymbol(t *testing.T, key model.SymbolRef) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := testPool.QueryRow(context.Background(), `SELECT owner FROM symbols WHERE authority=$1 AND symbol=$2`, key.Authority, key.Symbol).Scan(&id)
	require.NoError(t, err)
	return id
}

func assertEntryDoesNotExist(t *testing.T, key model.SymbolRef) {
	t.Helper()
	var count int
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT count(*) FROM symbols WHERE authority=$1 AND symbol=$2`, key.Authority, key.Symbol).Scan(&count))
	require.Zero(t, count)
}

func assertOwnedCount(t *testing.T, table string, entryID uuid.UUID, expected int) {
	t.Helper()
	ownerColumn := "entry"
	if table == "symbols" {
		ownerColumn = "owner"
	}
	query := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s=$1`, table, ownerColumn) //nolint:gosec // table names are fixed test constants
	if table == "address_components" {
		query = `SELECT count(*) FROM address_components ac JOIN addresses a ON a.id=ac.address WHERE a.entry=$1`
	}
	var count int
	require.NoError(t, testPool.QueryRow(context.Background(), query, entryID).Scan(&count))
	require.Equal(t, expected, count, table)
}

func requirePgCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	pgErr, ok := err.(*pgconn.PgError)
	require.True(t, ok, "expected PostgreSQL error, got %T: %v", err, err)
	require.Equal(t, code, pgErr.Code)
}
