package app

import (
	"context"
	"fmt"
	"github.com/indexdata/crosslink/broker/client"
	"github.com/indexdata/crosslink/broker/service"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

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

var HTTP_PORT = utils.GetEnvInt("HTTP_PORT", 8081)
var DB_TYPE = utils.GetEnv("DB_TYPE", "postgres")
var DB_USER = utils.GetEnv("DB_USER", "crosslink")
var DB_PASSWORD = utils.GetEnv("DB_PASSWORD", "crosslink")
var DB_HOST = utils.GetEnv("DB_HOST", "localhost")
var DB_PORT = utils.GetEnv("DB_PORT", "25432")
var DB_DATABASE = utils.GetEnv("DB_DATABASE", "crosslink")
var ConnectionString = fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=disable", DB_TYPE, DB_USER, DB_PASSWORD, DB_HOST, DB_PORT, DB_DATABASE)
var MigrationsFolder = "file://migrations"
var ENABLE_JSON_LOG = utils.GetEnv("ENABLE_JSON_LOG", "false")

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

func StartApp(illRepo ill_db.IllRepo, eventBus events.EventBus) {
	mux := http.NewServeMux()

	serviceHandler := http.HandlerFunc(HandleRequest)
	mux.Handle("/", serviceHandler)
	mux.HandleFunc("/healthz", HandleHealthz)

	mux.HandleFunc("/iso18626", handler.Iso18626PostHandler(illRepo, eventBus))

	appCtx.Logger().Info("Server started on http://localhost:" + strconv.Itoa(HTTP_PORT))
	http.ListenAndServe(":"+strconv.Itoa(HTTP_PORT), mux)
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

func AddDefaultHandlers(eventBus events.EventBus, iso18626Client client.Iso18626Client, supplierLocator service.SupplierLocator) {
	eventBus.HandleEventCreated(events.EventNameMessageSupplier, iso18626Client.MessageSupplier)
	eventBus.HandleEventCreated(events.EventNameMessageRequester, iso18626Client.MessageRequester)
	eventBus.HandleEventCreated(events.EventNameLocateSuppliers, supplierLocator.LocateSuppliers)
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
