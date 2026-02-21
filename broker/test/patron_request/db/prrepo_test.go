package db

import (
	"context"
	"math/big"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var prRepo pr_db.PrRepo
var appCtx = common.CreateExtCtxWithArgs(context.Background(), nil)

func TestMain(m *testing.M) {
	app.TENANT_TO_SYMBOL = ""
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	mockPort := utils.Must(test.GetFreePort())
	localAddress := "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
	test.Expect(os.Setenv("PEER_URL", localAddress), "failed to set peer URL")

	adapter.MOCK_CLIENT_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"

	apptest.StartMockApp(mockPort)

	ctx, cancel := context.WithCancel(context.Background())
	_, _, _, prRepo = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestItem(t *testing.T) {
	prId := uuid.NewString()
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: prId,
		Timestamp: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
	})
	assert.NoError(t, err)

	// Save works
	itemId := uuid.NewString()
	item, err := prRepo.SaveItem(appCtx, pr_db.SaveItemParams{
		ID:      itemId,
		PrID:    prId,
		Barcode: "b123",
		CallNumber: pgtype.Text{
			String: "c123",
			Valid:  true,
		},
		Title: pgtype.Text{
			String: "t123",
			Valid:  true,
		},
		ItemID: pgtype.Text{
			String: "i123",
			Valid:  true,
		},
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, itemId, item.ID)
	assert.Equal(t, prId, item.PrID)
	assert.Equal(t, "b123", item.Barcode)
	assert.Equal(t, "c123", item.CallNumber.String)
	assert.Equal(t, "t123", item.Title.String)
	assert.Equal(t, "i123", item.ItemID.String)
	assert.True(t, item.CreatedAt.Valid)

	// Update works
	item, err = prRepo.SaveItem(appCtx, pr_db.SaveItemParams{
		ID:      itemId,
		PrID:    prId,
		Barcode: "b12",
		CallNumber: pgtype.Text{
			String: "c12",
			Valid:  true,
		},
		Title: pgtype.Text{
			String: "t12",
			Valid:  true,
		},
		ItemID: pgtype.Text{
			String: "i12",
			Valid:  true,
		},
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now().Add(time.Hour),
			Valid: true,
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, itemId, item.ID)
	assert.Equal(t, prId, item.PrID)
	assert.Equal(t, "b12", item.Barcode)
	assert.Equal(t, "c12", item.CallNumber.String)
	assert.Equal(t, "t12", item.Title.String)
	assert.Equal(t, "i12", item.ItemID.String)
	assert.True(t, item.CreatedAt.Time.After(time.Now()))

	// Get by item id
	item, err = prRepo.GetItemById(appCtx, itemId)
	assert.NoError(t, err)
	assert.Equal(t, itemId, item.ID)

	// Get by pr id
	items, err := prRepo.GetItemByPrId(appCtx, prId)
	assert.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, itemId, items[0].ID)
}

func TestNotification(t *testing.T) {
	prId := uuid.NewString()
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: prId,
		Timestamp: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
	})
	assert.NoError(t, err)

	// Save works
	notificaitonId := uuid.NewString()
	notification, err := prRepo.SaveNotification(appCtx, pr_db.SaveNotificationParams{
		ID:         notificaitonId,
		PrID:       prId,
		FromSymbol: "f123",
		ToSymbol:   "t123",
		Side:       prservice.SideBorrowing,
		Note: pgtype.Text{
			String: "n123",
			Valid:  true,
		},
		Cost: pgtype.Numeric{
			Int:   big.NewInt(123),
			Exp:   -2,
			Valid: true,
		},
		Currency: pgtype.Text{
			String: "EUR",
			Valid:  true,
		},
		Condition: pgtype.Text{
			String: "c123",
			Valid:  true,
		},
		Receipt: pr_db.NotificationSeen,
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		AcknowledgedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, notificaitonId, notification.ID)
	assert.Equal(t, prId, notification.PrID)
	assert.Equal(t, "f123", notification.FromSymbol)
	assert.Equal(t, "t123", notification.ToSymbol)
	assert.Equal(t, prservice.SideBorrowing, notification.Side)
	assert.Equal(t, "n123", notification.Note.String)
	assert.Equal(t, "EUR", notification.Currency.String)
	assert.Equal(t, "c123", notification.Condition.String)
	assert.Equal(t, pr_db.NotificationSeen, notification.Receipt)
	cost, err := notification.Cost.Float64Value()
	assert.NoError(t, err)
	assert.Equal(t, 1.23, cost.Float64)
	assert.True(t, notification.CreatedAt.Valid)
	assert.True(t, notification.AcknowledgedAt.Valid)

	// Update works
	notification, err = prRepo.SaveNotification(appCtx, pr_db.SaveNotificationParams{
		ID:         notificaitonId,
		PrID:       prId,
		FromSymbol: "f12",
		ToSymbol:   "t12",
		Side:       prservice.SideLending,
		Note: pgtype.Text{
			String: "n12",
			Valid:  true,
		},
		Cost: pgtype.Numeric{
			Int:   big.NewInt(323),
			Exp:   -2,
			Valid: true,
		},
		Currency: pgtype.Text{
			String: "USD",
			Valid:  true,
		},
		Condition: pgtype.Text{
			String: "c12",
			Valid:  true,
		},
		Receipt: pr_db.NotificationAccepted,
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now().Add(time.Hour),
			Valid: true,
		},
		AcknowledgedAt: pgtype.Timestamp{
			Time:  time.Now().Add(time.Hour),
			Valid: true,
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, notificaitonId, notification.ID)
	assert.Equal(t, prId, notification.PrID)
	assert.Equal(t, "f12", notification.FromSymbol)
	assert.Equal(t, "t12", notification.ToSymbol)
	assert.Equal(t, prservice.SideLending, notification.Side)
	assert.Equal(t, "n12", notification.Note.String)
	assert.Equal(t, "USD", notification.Currency.String)
	assert.Equal(t, "c12", notification.Condition.String)
	assert.Equal(t, pr_db.NotificationAccepted, notification.Receipt)
	cost, err = notification.Cost.Float64Value()
	assert.NoError(t, err)
	assert.Equal(t, 3.23, cost.Float64)
	assert.True(t, notification.CreatedAt.Valid)
	assert.True(t, notification.AcknowledgedAt.Valid)

	// Get by notification id
	notification, err = prRepo.GetNotificationById(appCtx, notificaitonId)
	assert.NoError(t, err)
	assert.Equal(t, notificaitonId, notification.ID)

	// Get by pr id
	notifications, err := prRepo.GetNotificationsByPrId(appCtx, prId)
	assert.NoError(t, err)
	assert.Len(t, notifications, 1)
	assert.Equal(t, notificaitonId, notifications[0].ID)
}
