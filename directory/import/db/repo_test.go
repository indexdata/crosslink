package importdb_test

import (
	"context"
	"fmt"
	"os"
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

	_, err = testPool.Exec(ctx, `INSERT INTO networks (consortium, name, priority) VALUES ($1, NULL, 0)`, consortiumID)
	requirePgCode(t, err, "23502")
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
		Data: model.NetworkData{Priority: 1, Reciprocal: &reciprocal, Entries: []model.SymbolRef{first}},
	}

	result, err := repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicyFail)
	require.NoError(t, err)
	require.Equal(t, model.OutcomeImported, result.Outcome)
	id := networkIDByKey(t, consortium, "Main")
	require.NotEqual(t, uuid.Nil, id)
	require.Equal(t, []model.SymbolRef{first}, networkAssignments(t, id))

	_, err = repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicyFail)
	require.ErrorContains(t, err, "already exists")
	skipped, err := repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicySkip)
	require.NoError(t, err)
	require.Equal(t, model.OutcomeSkipped, skipped.Outcome)

	aggregate.Data.Priority = 2
	aggregate.Data.Reciprocal = nil
	aggregate.Data.Entries = []model.SymbolRef{second}
	_, err = repo.ImportNetwork(context.Background(), aggregate, model.ConflictPolicyUpdate)
	require.NoError(t, err)
	require.Equal(t, id, networkIDByKey(t, consortium, "Main"))
	require.Equal(t, []model.SymbolRef{second}, networkAssignments(t, id))
}

func TestImportNetworkRejectsNonConsortiumOwner(t *testing.T) {
	repo, _, first, _ := importRepoFixture(t)
	aggregate := model.NetworkAggregate{
		Key:  model.NetworkKey{Consortium: first, Name: "Main"},
		Data: model.NetworkData{Entries: []model.SymbolRef{}},
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

func networkAssignments(t *testing.T, id uuid.UUID) []model.SymbolRef {
	t.Helper()
	return aggregateAssignments(t, "entry_networks", "network", id)
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
