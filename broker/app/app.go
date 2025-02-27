package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/jackc/pgx/v5"

	"github.com/indexdata/crosslink/broker/client"
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
var INIT_DATA = utils.GetEnv("INIT_DATA", "true")
var HOLDINGS_ADAPTER = utils.GetEnv("HOLDINGS_ADAPTER", "mock")
var SRU_URL = utils.GetEnv("SRU_URL", "http://localhost:8081/sru")

var appCtx = extctx.CreateExtCtxWithLogArgsAndHandler(context.Background(), nil, configLog())

func configLog() slog.Handler {
	if strings.EqualFold(ENABLE_JSON_LOG, "true") {
		jsonHandler := slog.NewJSONHandler(os.Stdout, nil)
		extctx.DefaultLogHandler = jsonHandler
		return jsonHandler
	} else {
		return extctx.DefaultLogHandler
	}
}

func Init(ctx context.Context) (events.EventBus, ill_db.IllRepo, events.EventRepo, error) {
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
	if err != nil {
		return nil, nil, nil, err
	}
	supplierLocator := service.CreateSupplierLocator(eventBus, illRepo, new(adapter.MockDirectoryLookupAdapter), holdingsAdapter)
	workflowManager := service.CreateWorkflowManager(eventBus)
	AddDefaultHandlers(eventBus, iso18626Client, supplierLocator, workflowManager)
	StartEventBus(ctx, eventBus)
	return eventBus, illRepo, eventRepo, nil
}

func Run(ctx context.Context) error {
	eventBus, illRepo, eventRepo, err := Init(ctx)
	if err != nil {
		return err
	}
	return StartServer(illRepo, eventRepo, eventBus)
}

func StartServer(illRepo ill_db.IllRepo, eventRepo events.EventRepo, eventBus events.EventBus) error {
	if strings.EqualFold(INIT_DATA, "true") {
		initData(illRepo)
	}
	mux := http.NewServeMux()

	serviceHandler := http.HandlerFunc(HandleRequest)
	mux.Handle("/", serviceHandler)
	mux.HandleFunc("/healthz", HandleHealthz)

	mux.HandleFunc("/iso18626", handler.Iso18626PostHandler(illRepo, eventBus))
	mux.HandleFunc("/v3/open-api.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		http.ServeFile(w, r, "handler/open-api.yaml")
	})

	apiHandler := api.NewApiHandler(eventRepo, illRepo)
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

	// Migrate up
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		appCtx.Logger().Error("failed to run migration", "error", err)
		return
	}
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

func initData(illRepo ill_db.IllRepo) {
	peer, err := illRepo.GetPeerBySymbol(appCtx, "isil:req")
	if err == nil {
		err = illRepo.DeletePeer(appCtx, peer.ID)
		if err != nil {
			panic(err)
		}
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			panic(err)
		}
	}
	utils.Warn(illRepo.SavePeer(appCtx, ill_db.SavePeerParams{
		ID:            "d3b07384-d9a0-4c1e-8f8e-4f8e4f8e4f8e",
		Name:          "Requester",
		Symbol:        "isil:req",
		Url:           adapter.MOCK_CLIENT_URL,
		RefreshPolicy: ill_db.RefreshPolicyNever,
	}))
}
