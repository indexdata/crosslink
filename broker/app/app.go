package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/client"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/indexdata/crosslink/broker/service"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgxpool"
)

var HTTP_PORT = utils.Must(utils.GetEnvInt("HTTP_PORT", 8081))
var DB_TYPE = utils.GetEnv("DB_TYPE", "postgres")
var DB_USER = utils.GetEnv("DB_USER", "crosslink")
var DB_PASSWORD = utils.GetEnv("DB_PASSWORD", "crosslink")
var DB_HOST = utils.GetEnv("DB_HOST", "localhost")
var DB_PORT = utils.GetEnv("DB_PORT", "25432")
var DB_DATABASE = utils.GetEnv("DB_DATABASE", "crosslink")
var ConnectionString = fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=disable", DB_TYPE, DB_USER, DB_PASSWORD, DB_HOST, DB_PORT, DB_DATABASE)
var MigrationsFolder = "file://migrations"
var ENABLE_JSON_LOG = utils.GetEnv("ENABLE_JSON_LOG", "false")
var HOLDINGS_ADAPTER = utils.GetEnv("HOLDINGS_ADAPTER", "mock")
var SRU_URL = utils.GetEnv("SRU_URL", "http://localhost:8081/sru")

var appCtx = extctx.CreateExtCtxWithLogArgsAndHandler(context.Background(), nil, configLog())

type Context struct {
	EventBus   events.EventBus
	IllRepo    ill_db.IllRepo
	EventRepo  events.EventRepo
	DirAdapter adapter.DirectoryLookupAdapter
}

func configLog() slog.Handler {
	if strings.EqualFold(ENABLE_JSON_LOG, "true") {
		jsonHandler := slog.NewJSONHandler(os.Stdout, nil)
		extctx.DefaultLogHandler = jsonHandler
		return jsonHandler
	} else {
		return extctx.DefaultLogHandler
	}
}

func Init(ctx context.Context) (Context, error) {
	RunMigrateScripts()
	pool := InitDbPool()
	eventRepo := CreateEventRepo(pool)
	eventBus := CreateEventBus(eventRepo)
	illRepo := CreateIllRepo(pool)
	iso18626Client := client.CreateIso18626Client(eventBus, illRepo)

	holdingsAdapter, err := adapter.CreateHoldingsLookupAdapter(map[string]string{
		adapter.HoldingsAdapter: HOLDINGS_ADAPTER,
		adapter.SruUrl:          SRU_URL,
	})
	dirAdapter := new(adapter.MockDirectoryLookupAdapter)
	if err != nil {
		return Context{}, err
	}
	supplierLocator := service.CreateSupplierLocator(eventBus, illRepo, dirAdapter, holdingsAdapter)
	workflowManager := service.CreateWorkflowManager(eventBus, illRepo)
	AddDefaultHandlers(eventBus, iso18626Client, supplierLocator, workflowManager)
	StartEventBus(ctx, eventBus)
	return Context{
		EventBus:   eventBus,
		IllRepo:    illRepo,
		EventRepo:  eventRepo,
		DirAdapter: dirAdapter,
	}, nil
}

func Run(ctx context.Context) error {
	context, err := Init(ctx)
	if err != nil {
		return err
	}
	return StartServer(context)
}

func StartServer(context Context) error {
	mux := http.NewServeMux()

	serviceHandler := http.HandlerFunc(HandleRequest)
	mux.Handle("/", serviceHandler)
	mux.HandleFunc("/healthz", HandleHealthz)

	mux.HandleFunc("/iso18626", handler.Iso18626PostHandler(context.IllRepo, context.EventBus, context.DirAdapter))
	mux.HandleFunc("/v3/open-api.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		http.ServeFile(w, r, "handler/open-api.yaml")
	})

	apiHandler := api.NewApiHandler(context.EventRepo, context.IllRepo)
	oapi.HandlerFromMux(&apiHandler, mux)

	appCtx.Logger().Info("Server started on http://localhost:" + strconv.Itoa(HTTP_PORT))
	return http.ListenAndServe(":"+strconv.Itoa(HTTP_PORT), mux)
}

func RunMigrateScripts() {
	m, err := migrate.New(MigrationsFolder, ConnectionString)
	if err != nil {
		appCtx.Logger().Error("failed to initiate migration", "error", err)
		return
	}

	// Check migration version before running
	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		appCtx.Logger().Error("failed to get migration version", "error", err)
		return
	}
	appCtx.Logger().Info("current migration version", "version", version, "dirty", dirty)

	// Migrate up
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		appCtx.Logger().Error("failed to run migration", "error", err)
		return
	}

	// Check migration version after running
	version, dirty, err = m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		appCtx.Logger().Error("failed to get migration version after running", "error", err)
		return
	}
	appCtx.Logger().Info("migration version after running", "version", version, "dirty", dirty)
}

func InitDbPool() *pgxpool.Pool {
	dbPool, err := pgxpool.New(context.Background(), ConnectionString)
	if err != nil {
		appCtx.Logger().Error("Unable to create pool to database", "error", err)
		os.Exit(1)
	}
	return dbPool
}

func CreateEventRepo(dbPool *pgxpool.Pool) events.EventRepo {
	eventRepo := new(events.PgEventRepo)
	eventRepo.Pool = dbPool
	return eventRepo
}

func CreateEventBus(eventRepo events.EventRepo) events.EventBus {
	eventBus := events.NewPostgresEventBus(eventRepo, ConnectionString)
	return eventBus
}

func AddDefaultHandlers(eventBus events.EventBus, iso18626Client client.Iso18626Client, supplierLocator service.SupplierLocator, workflowManager service.WorkflowManager) {
	eventBus.HandleEventCreated(events.EventNameMessageSupplier, iso18626Client.MessageSupplier)
	eventBus.HandleEventCreated(events.EventNameMessageRequester, iso18626Client.MessageRequester)

	eventBus.HandleEventCreated(events.EventNameLocateSuppliers, supplierLocator.LocateSuppliers)
	eventBus.HandleEventCreated(events.EventNameSelectSupplier, supplierLocator.SelectSupplier)

	eventBus.HandleEventCreated(events.EventNameRequestReceived, workflowManager.RequestReceived)
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, workflowManager.SupplierMessageReceived)
	eventBus.HandleEventCreated(events.EventNameRequesterMsgReceived, workflowManager.RequesterMessageReceived)
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, workflowManager.OnLocateSupplierComplete)
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, workflowManager.OnSelectSupplierComplete)
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, workflowManager.OnMessageSupplierComplete)
}
func StartEventBus(ctx context.Context, eventBus events.EventBus) {
	err := eventBus.Start(extctx.CreateExtCtxWithArgs(ctx, nil))
	if err != nil {
		appCtx.Logger().Error("Unable to listen to database notify", "error", err)
		os.Exit(1)
	}
}

func CreateIllRepo(dbPool *pgxpool.Pool) ill_db.IllRepo {
	illRepo := new(ill_db.PgIllRepo)
	illRepo.Pool = dbPool
	return illRepo
}

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello from " + r.URL.Path))
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}
