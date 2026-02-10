package app

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	prapi "github.com/indexdata/crosslink/broker/patron_request/api"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"

	"github.com/dustin/go-humanize"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/client"
	"github.com/indexdata/crosslink/broker/dbutil"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/vcs"

	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/indexdata/crosslink/broker/lms"
)

var HTTP_PORT = utils.Must(utils.GetEnvInt("HTTP_PORT", 8081))
var DB_TYPE = utils.GetEnv("DB_TYPE", "postgres")
var DB_USER = utils.GetEnv("DB_USER", "crosslink")
var DB_PASSWORD = utils.GetEnv("DB_PASSWORD", "crosslink")
var DB_HOST = utils.GetEnv("DB_HOST", "localhost")
var DB_PORT = utils.GetEnv("DB_PORT", "25432")
var DB_DATABASE = utils.GetEnv("DB_DATABASE", "crosslink")
var ConnectionString = dbutil.GetConnectionString(DB_TYPE, DB_USER, DB_PASSWORD, DB_HOST, DB_PORT, DB_DATABASE)
var API_PAGE_SIZE int32 = int32(utils.Must(utils.GetEnvInt("API_PAGE_SIZE", int(api.LIMIT_DEFAULT))))
var MigrationsFolder = "file://migrations"
var ENABLE_JSON_LOG = utils.GetEnv("ENABLE_JSON_LOG", "false")
var LOG_LEVEL = utils.GetEnv("LOG_LEVEL", "INFO")
var HOLDINGS_ADAPTER = utils.GetEnv("HOLDINGS_ADAPTER", "mock")
var SRU_URL = utils.GetEnv("SRU_URL", "http://localhost:8081/sru")
var DIRECTORY_ADAPTER = utils.GetEnv("DIRECTORY_ADAPTER", "mock")
var DIRECTORY_API_URL = utils.GetEnv("DIRECTORY_API_URL", "http://localhost:8081/directory/entries")
var MAX_MESSAGE_SIZE, _ = utils.GetEnvAny("MAX_MESSAGE_SIZE", int(100*1024), func(val string) (int, error) {
	v, err := humanize.ParseBytes(val)
	if err != nil && v > uint64(math.MaxInt) {
		appCtx.Logger().Error("MAX_MESSAGE_SIZE value is too large, using default")
		return 0, fmt.Errorf("value %s is too large", val)
	}
	return int(v), err
})
var BROKER_MODE = utils.GetEnv("BROKER_MODE", "opaque")
var TENANT_TO_SYMBOL = os.Getenv("TENANT_TO_SYMBOL")
var CLIENT_DELAY = utils.GetEnv("CLIENT_DELAY", "0ms")
var SHUTDOWN_DELAY, _ = utils.GetEnvAny("SHUTDOWN_DELAY", time.Duration(15*float64(time.Second)), func(val string) (time.Duration, error) {
	d, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf("invalid SHUTDOWN_DELAY value: %s", val)
	}
	return d, nil
})

var ServeMux *http.ServeMux
var appCtx = common.CreateExtCtxWithLogArgsAndHandler(context.Background(), nil, configLog())

type Context struct {
	EventBus     events.EventBus
	IllRepo      ill_db.IllRepo
	EventRepo    events.EventRepo
	DirAdapter   adapter.DirectoryLookupAdapter
	PrRepo       pr_db.PrRepo
	PrApiHandler prapi.PatronRequestApiHandler
	SseBroker    *api.SseBroker
}

func configLog() slog.Handler {
	var level slog.Level
	switch strings.ToUpper(LOG_LEVEL) {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{
		Level: level,
	}
	if strings.EqualFold(ENABLE_JSON_LOG, "true") {
		jsonHandler := slog.NewJSONHandler(os.Stdout, opts)
		common.DefaultLogHandler = jsonHandler
		return jsonHandler
	} else {
		textHandler := slog.NewTextHandler(os.Stdout, opts)
		common.DefaultLogHandler = textHandler
		return textHandler
	}
}

func Init(ctx context.Context) (Context, error) {
	appCtx.Logger().Info("starting " + vcs.GetSignature())
	holdingsAdapter, err := adapter.CreateHoldingsLookupAdapter(map[string]string{
		adapter.HoldingsAdapter: HOLDINGS_ADAPTER,
		adapter.SruUrl:          SRU_URL,
	})
	if err != nil {
		return Context{}, err
	}

	adapter.DEFAULT_BROKER_MODE = getBrokerMode(BROKER_MODE)
	dirAdapter, err := adapter.CreateDirectoryLookupAdapter(map[string]string{
		adapter.DirectoryAdapter: DIRECTORY_ADAPTER,
		adapter.DirectoryApiUrl:  DIRECTORY_API_URL,
	})
	if err != nil {
		return Context{}, err
	}

	delay, err := time.ParseDuration(CLIENT_DELAY)
	if err != nil {
		return Context{}, err
	}

	err = RunMigrateScripts()
	if err != nil {
		return Context{}, err
	}

	pool, err := InitDbPool()
	if err != nil {
		return Context{}, err
	}

	eventRepo := CreateEventRepo(pool)
	eventBus := CreateEventBus(eventRepo)
	illRepo := CreateIllRepo(pool)
	prRepo := CreatePrRepo(pool)

	prMessageHandler := prservice.CreatePatronRequestMessageHandler(prRepo, eventRepo, illRepo, eventBus)
	iso18626Client := client.CreateIso18626Client(eventBus, illRepo, prMessageHandler, MAX_MESSAGE_SIZE, delay)
	iso18626Handler := handler.CreateIso18626Handler(eventBus, eventRepo, illRepo, dirAdapter)
	supplierLocator := service.CreateSupplierLocator(eventBus, illRepo, dirAdapter, holdingsAdapter)
	workflowManager := service.CreateWorkflowManager(eventBus, illRepo, service.WorkflowConfig{})
	lmsCreator := lms.NewLmsCreator(illRepo, dirAdapter)
	prActionService := prservice.CreatePatronRequestActionService(prRepo, eventBus, &iso18626Handler, lmsCreator)
	prApiHandler := prapi.NewPrApiHandler(prRepo, eventBus, eventRepo, common.NewTenant(TENANT_TO_SYMBOL), API_PAGE_SIZE)

	sseBroker := api.NewSseBroker(appCtx, common.NewTenant(TENANT_TO_SYMBOL))

	AddDefaultHandlers(eventBus, iso18626Client, supplierLocator, workflowManager, iso18626Handler, *prActionService, prApiHandler, sseBroker)
	err = StartEventBus(ctx, eventBus)
	if err != nil {
		return Context{}, err
	}
	return Context{
		EventBus:     eventBus,
		IllRepo:      illRepo,
		EventRepo:    eventRepo,
		DirAdapter:   dirAdapter,
		PrRepo:       prRepo,
		PrApiHandler: prApiHandler,
		SseBroker:    sseBroker,
	}, nil
}

func Run(ctx context.Context) error {
	context, err := Init(ctx)
	if err != nil {
		return err
	}
	return StartServer(context)
}

func StartServer(ctx Context) error {
	ServeMux = http.NewServeMux()
	ServeMux.HandleFunc("GET /healthz", HandleHealthz)
	//all methods must be mapped explicitly to avoid conflicts with the index handler
	ServeMux.HandleFunc("GET /iso18626", handler.Iso18626PostHandler(ctx.IllRepo, ctx.EventBus, ctx.DirAdapter, MAX_MESSAGE_SIZE))
	ServeMux.HandleFunc("POST /iso18626", handler.Iso18626PostHandler(ctx.IllRepo, ctx.EventBus, ctx.DirAdapter, MAX_MESSAGE_SIZE))
	ServeMux.HandleFunc("PUT /iso18626", handler.Iso18626PostHandler(ctx.IllRepo, ctx.EventBus, ctx.DirAdapter, MAX_MESSAGE_SIZE))
	ServeMux.HandleFunc("DELETE /iso18626", handler.Iso18626PostHandler(ctx.IllRepo, ctx.EventBus, ctx.DirAdapter, MAX_MESSAGE_SIZE))
	ServeMux.HandleFunc("GET /v3/open-api.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		http.ServeFile(w, r, "oapi/open-api.yaml")
	})

	apiHandler := api.NewApiHandler(ctx.EventRepo, ctx.IllRepo, common.NewTenant(""), API_PAGE_SIZE)
	oapi.HandlerFromMux(&apiHandler, ServeMux)
	proapi.HandlerFromMux(&ctx.PrApiHandler, ServeMux)
	if TENANT_TO_SYMBOL != "" {
		apiHandler := api.NewApiHandler(ctx.EventRepo, ctx.IllRepo, common.NewTenant(TENANT_TO_SYMBOL), API_PAGE_SIZE)
		oapi.HandlerFromMuxWithBaseURL(&apiHandler, ServeMux, "/broker")
		proapi.HandlerFromMuxWithBaseURL(&ctx.PrApiHandler, ServeMux, "/broker")
	}

	// SSE Incoming message handler
	ServeMux.HandleFunc("/sse/events", ctx.SseBroker.ServeHTTP)

	signatureHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", vcs.GetSignature())
		ServeMux.ServeHTTP(w, r)
	})
	server := &http.Server{
		Addr:              ":" + strconv.Itoa(HTTP_PORT),
		Handler:           signatureHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// channel to listen for server errors
	serverErrors := make(chan error, 1)
	go func() {
		appCtx.Logger().Info("HTTP server started on port " + strconv.Itoa(HTTP_PORT))
		serverErrors <- server.ListenAndServe()
	}()
	// channel to listen for OS signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	// block until we receive a signal or server error
	select {
	case err := <-serverErrors:
		return fmt.Errorf("HTTP server error: %w", err)
	case sig := <-shutdown:
		appCtx.Logger().Info("HTTP server shutdown initiated", "signal", sig)
		// give outstanding requests a timeout to complete
		ctx, cancel := context.WithTimeout(appCtx, SHUTDOWN_DELAY)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			server.Close()
			return fmt.Errorf("HTTP server could not shutdown gracefully: %w", err)
		}
		appCtx.Logger().Info("HTTP server shutdown complete")
		return nil
	}
}

func RunMigrateScripts() error {
	verFrom, verTo, dirty, err := dbutil.RunMigrateScripts(MigrationsFolder, ConnectionString)
	if err != nil {
		return fmt.Errorf("DB migration failed: err=%w versionFrom=%d versionTo=%d dirty=%t", err, verFrom, verTo, dirty)
	}
	appCtx.Logger().Info("DB migration success", "versionFrom", verFrom, "versionTo", verTo, "dirty", dirty)
	return nil
}

func InitDbPool() (*pgxpool.Pool, error) {
	dbPool, err := dbutil.InitDbPool(ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("unable to create pool to database: %w", err)
	}
	return dbPool, nil
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

func AddDefaultHandlers(eventBus events.EventBus, iso18626Client client.Iso18626Client,
	supplierLocator service.SupplierLocator, workflowManager service.WorkflowManager, iso18626Handler handler.Iso18626Handler,
	prActionService prservice.PatronRequestActionService, prApiHandler prapi.PatronRequestApiHandler, sseBroker *api.SseBroker) {
	eventBus.HandleEventCreated(events.EventNameMessageSupplier, iso18626Client.MessageSupplier)
	eventBus.HandleEventCreated(events.EventNameMessageRequester, iso18626Client.MessageRequester)
	eventBus.HandleEventCreated(events.EventNameConfirmRequesterMsg, iso18626Handler.ConfirmRequesterMsg)
	eventBus.HandleEventCreated(events.EventNameConfirmSupplierMsg, iso18626Handler.ConfirmSupplierMsg)

	eventBus.HandleEventCreated(events.EventNameLocateSuppliers, supplierLocator.LocateSuppliers)
	eventBus.HandleEventCreated(events.EventNameSelectSupplier, supplierLocator.SelectSupplier)

	eventBus.HandleEventCreated(events.EventNameRequestReceived, workflowManager.RequestReceived)
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, workflowManager.SupplierMessageReceived)
	eventBus.HandleEventCreated(events.EventNameRequesterMsgReceived, workflowManager.RequesterMessageReceived)
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, workflowManager.OnLocateSupplierComplete)
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, workflowManager.OnSelectSupplierComplete)
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, workflowManager.OnMessageSupplierComplete)
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, workflowManager.OnMessageRequesterComplete)
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, sseBroker.IncomingIsoMessage)
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, sseBroker.IncomingIsoMessage)

	eventBus.HandleEventCreated(events.EventNameInvokeAction, prActionService.InvokeAction)
	eventBus.HandleTaskCompleted(events.EventNameInvokeAction, prApiHandler.ConfirmActionProcess)
}
func StartEventBus(ctx context.Context, eventBus events.EventBus) error {
	err := eventBus.Start(common.CreateExtCtxWithArgs(ctx, nil))
	if err != nil {
		return fmt.Errorf("starting event bus failed err=%w", err)
	}
	return nil
}

func CreateIllRepo(dbPool *pgxpool.Pool) ill_db.IllRepo {
	illRepo := new(ill_db.PgIllRepo)
	illRepo.Pool = dbPool
	return illRepo
}

func CreatePrRepo(dbPool *pgxpool.Pool) pr_db.PrRepo {
	illRepo := new(pr_db.PgPrRepo)
	illRepo.Pool = dbPool
	return illRepo
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}

func getBrokerMode(mode string) common.BrokerMode {
	if strings.EqualFold(mode, string(common.BrokerModeTransparent)) {
		return common.BrokerModeTransparent
	} else {
		return common.BrokerModeOpaque
	}
}
