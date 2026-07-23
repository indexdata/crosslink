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
	"github.com/indexdata/crosslink/iso18626"
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
	app.DB_PROVISION = true

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	app.DB_EXPLAIN_ANALYZE = true
	mockPort := utils.Must(test.GetFreePort())
	localAddress := "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
	test.Expect(os.Setenv("PEER_URL", localAddress), "failed to set peer URL")

	adapter.MOCK_PEER_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"

	apptest.StartMockApp(mockPort)

	ctx, cancel := context.WithCancel(context.Background())
	_, _, _, prRepo = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(test.TerminatePGContainer(ctx, pgContainer), "failed to stop db container")
	os.Exit(code)
}

func TestItem(t *testing.T) {
	prId := uuid.NewString()
	pr, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: prId,
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		Language:      "english",
		Items:         []pr_db.PrItem{},
		TerminalState: false,
	})
	assert.NoError(t, err)
	assert.True(t, pr.UpdatedAt.Valid)
	assert.Equal(t, pr.CreatedAt.Time, pr.UpdatedAt.Time)

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
	items, err := prRepo.GetItemsByPrId(appCtx, prId)
	assert.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, itemId, items[0].ID)

	err = prRepo.DeleteItemById(appCtx, itemId)
	assert.NoError(t, err)

	err = prRepo.DeletePatronRequest(appCtx, prId)
	assert.NoError(t, err)
}

func TestTemplateUpdatedAtInitializedFromCreatedAt(t *testing.T) {
	createdAt := pgtype.Timestamp{Time: time.Now(), Valid: true}
	template, err := prRepo.SaveTemplate(appCtx, pr_db.SaveTemplateParams{
		ID:          uuid.NewString(),
		Owner:       "ISIL:TEST",
		Title:       "Test template",
		Purpose:     "general",
		Body:        "Body",
		ContentType: "text/plain",
		Labels:      []string{},
		CreatedAt:   createdAt,
	})

	assert.NoError(t, err)
	assert.True(t, template.UpdatedAt.Valid)
	assert.Equal(t, template.CreatedAt.Time, template.UpdatedAt.Time)
}

func TestNotification(t *testing.T) {
	prId := uuid.NewString()
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: prId,
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		Language:      "english",
		Items:         []pr_db.PrItem{},
		TerminalState: false,
	})
	assert.NoError(t, err)

	// Save works
	notificaitonId := uuid.NewString()
	notification, err := prRepo.SaveNotification(appCtx, pr_db.SaveNotificationParams{
		ID:         notificaitonId,
		PrID:       prId,
		FromSymbol: "f123",
		ToSymbol:   "t123",
		Direction:  pr_db.NotificationDirectionReceived,
		Kind:       pr_db.NotificationKindCondition,
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
	assert.Equal(t, pr_db.NotificationDirectionReceived, notification.Direction)
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
		Direction:  pr_db.NotificationDirectionSent,
		Kind:       pr_db.NotificationKindCondition,
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
	assert.Equal(t, pr_db.NotificationDirectionSent, notification.Direction)
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
	var fullCount int64
	notifications, fullCount, err := prRepo.GetNotificationsByPrId(appCtx, pr_db.GetNotificationsByPrIdParams{PrID: prId, Limit: 10, Offset: 0, Kind: ""})
	assert.NoError(t, err)
	assert.Len(t, notifications, 1)
	assert.Equal(t, notificaitonId, notifications[0].ID)
	assert.Equal(t, int64(1), fullCount)

	err = prRepo.DeleteNotificationById(appCtx, notificaitonId)
	assert.NoError(t, err)

	err = prRepo.DeletePatronRequest(appCtx, prId)
	assert.NoError(t, err)
}

func TestMarkConditionNotificationsReceipt(t *testing.T) {
	prId := uuid.NewString()
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: prId,
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		Language:      "english",
		Items:         []pr_db.PrItem{},
		TerminalState: false,
	})
	assert.NoError(t, err)

	acknowledgedAt := pgtype.Timestamp{Time: time.Now().Add(-time.Hour), Valid: true}
	notificationsToCreate := []pr_db.SaveNotificationParams{
		{
			ID:         uuid.NewString(),
			PrID:       prId,
			FromSymbol: "ISIL:SUP",
			ToSymbol:   "ISIL:REQ",
			Direction:  pr_db.NotificationDirectionSent,
			Kind:       pr_db.NotificationKindCondition,
			Receipt:    pr_db.NotificationSent,
			CreatedAt:  pgtype.Timestamp{Time: time.Now(), Valid: true},
		},
		{
			ID:             uuid.NewString(),
			PrID:           prId,
			FromSymbol:     "ISIL:SUP",
			ToSymbol:       "ISIL:REQ",
			Direction:      pr_db.NotificationDirectionSent,
			Kind:           pr_db.NotificationKindCondition,
			Receipt:        pr_db.NotificationSeen,
			CreatedAt:      pgtype.Timestamp{Time: time.Now().Add(time.Second), Valid: true},
			AcknowledgedAt: acknowledgedAt,
		},
		{
			ID:         uuid.NewString(),
			PrID:       prId,
			FromSymbol: "ISIL:SUP",
			ToSymbol:   "ISIL:REQ",
			Direction:  pr_db.NotificationDirectionSent,
			Kind:       pr_db.NotificationKindCondition,
			Receipt:    pr_db.NotificationRejected,
			CreatedAt:  pgtype.Timestamp{Time: time.Now().Add(2 * time.Second), Valid: true},
		},
		{
			ID:         uuid.NewString(),
			PrID:       prId,
			FromSymbol: "ISIL:REQ",
			ToSymbol:   "ISIL:SUP",
			Direction:  pr_db.NotificationDirectionReceived,
			Kind:       pr_db.NotificationKindCondition,
			CreatedAt:  pgtype.Timestamp{Time: time.Now().Add(3 * time.Second), Valid: true},
		},
		{
			ID:         uuid.NewString(),
			PrID:       prId,
			FromSymbol: "ISIL:SUP",
			ToSymbol:   "ISIL:REQ",
			Direction:  pr_db.NotificationDirectionSent,
			Kind:       pr_db.NotificationKindCondition,
			Receipt:    pr_db.NotificationFailedToSend,
			CreatedAt:  pgtype.Timestamp{Time: time.Now().Add(4 * time.Second), Valid: true},
		},
		{
			ID:         uuid.NewString(),
			PrID:       prId,
			FromSymbol: "ISIL:SUP",
			ToSymbol:   "ISIL:REQ",
			Direction:  pr_db.NotificationDirectionSent,
			Kind:       pr_db.NotificationKindNote,
			CreatedAt:  pgtype.Timestamp{Time: time.Now().Add(5 * time.Second), Valid: true},
		},
	}
	for _, notification := range notificationsToCreate {
		_, err = prRepo.SaveNotification(appCtx, notification)
		assert.NoError(t, err)
	}

	err = prRepo.MarkConditionNotificationsReceipt(appCtx, pr_db.MarkConditionNotificationsReceiptParams{
		Receipt:   string(pr_db.NotificationAccepted),
		PrID:      prId,
		Direction: string(pr_db.NotificationDirectionSent),
	})
	assert.NoError(t, err)

	notifications, _, err := prRepo.GetNotificationsByPrId(appCtx, pr_db.GetNotificationsByPrIdParams{PrID: prId, Limit: 10, Offset: 0, Kind: ""})
	assert.NoError(t, err)
	byID := map[string]pr_db.Notification{}
	for _, notification := range notifications {
		byID[notification.ID] = notification
	}
	assert.Equal(t, pr_db.NotificationAccepted, byID[notificationsToCreate[0].ID].Receipt)
	assert.True(t, byID[notificationsToCreate[0].ID].AcknowledgedAt.Valid)
	assert.Equal(t, pr_db.NotificationAccepted, byID[notificationsToCreate[1].ID].Receipt)
	assert.Equal(t, acknowledgedAt.Time.Format(time.DateTime), byID[notificationsToCreate[1].ID].AcknowledgedAt.Time.Format(time.DateTime))
	assert.Equal(t, pr_db.NotificationRejected, byID[notificationsToCreate[2].ID].Receipt)
	assert.False(t, byID[notificationsToCreate[2].ID].AcknowledgedAt.Valid)
	assert.Empty(t, byID[notificationsToCreate[3].ID].Receipt)
	assert.Equal(t, pr_db.NotificationFailedToSend, byID[notificationsToCreate[4].ID].Receipt)
	assert.False(t, byID[notificationsToCreate[4].ID].AcknowledgedAt.Valid)
	assert.Empty(t, byID[notificationsToCreate[5].ID].Receipt)

	for _, notification := range notificationsToCreate {
		err = prRepo.DeleteNotificationById(appCtx, notification.ID)
		assert.NoError(t, err)
	}
	err = prRepo.DeletePatronRequest(appCtx, prId)
	assert.NoError(t, err)
}

func TestListPatronRequests(t *testing.T) {
	prIds := []string{}

	// Create 2 requests; only the first carries an internal note
	for i := 0; i < 2; i++ {
		prId := uuid.NewString()
		prIds = append(prIds, prId)
		var internalNote pgtype.Text
		if i == 0 {
			internalNote = pgtype.Text{String: "staff only", Valid: true}
		}
		_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
			ID:           prId,
			InternalNote: internalNote,
			CreatedAt: pgtype.Timestamp{
				Time:  time.Now(),
				Valid: true,
			},
			Side: prservice.SideBorrowing,
			RequesterSymbol: pgtype.Text{
				String: "ISIL:REQ",
				Valid:  true,
			},
			SupplierSymbol: pgtype.Text{
				String: "ISIL:SUP",
				Valid:  true,
			},
			State:    prservice.BorrowerStateValidated,
			Language: "english",
			RequesterReqID: pgtype.Text{
				String: "REQ-123",
				Valid:  true,
			},
			Patron: pgtype.Text{
				String: "P456",
				Valid:  true,
			},
			IllRequest: iso18626.Request{
				BibliographicInfo: iso18626.BibliographicInfo{
					Title:  "Do Androids Dream of Electric Sheep?",
					Author: "Ray Bradbury",
					BibliographicItemId: []iso18626.BibliographicItemId{
						{
							BibliographicItemIdentifier: "978-3-16-148410-0",
							BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{
								Text: "ISBN",
							},
						},
						{
							BibliographicItemIdentifier: "2049-3630",
							BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{
								Text: "ISSN",
							},
						},
						{
							BibliographicItemIdentifier: "1234-567X",
							BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{
								Text: "ISSN",
							},
						},
					},
				},
				PatronInfo: &iso18626.PatronInfo{
					GivenName: "John",
					Surname:   "Doe",
					PatronId:  "PP-789",
				},
			},
			Items:         []pr_db.PrItem{},
			TerminalState: false,
		})
		assert.NoError(t, err)
	}
	cql := "title = Androids"
	pgcql, err := pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err := prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  1,
		Offset: 0,
	}, pgcql)

	assert.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, int64(2), fullCount)

	cql = "requester_symbol = isil:req"
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, int64(2), fullCount)

	cql = "supplier_symbol = isil:sup"
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, int64(2), fullCount)

	cql = "requester_req_id = req-123"
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, int64(2), fullCount)

	cql = `isbn = "978-3-16-148410-0"`
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, int64(2), fullCount)

	cql = `issn = "2049-3630"`
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, int64(2), fullCount)

	cql = `isbn = "9783161484100"`
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, int64(2), fullCount)

	cql = `issn = "20493630"`
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, int64(2), fullCount)

	cql = `issn = "1234567x"`
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, int64(2), fullCount)

	// not found
	cql = "title = banners"
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)

	assert.NoError(t, err)
	assert.Len(t, list, 0)
	assert.Equal(t, int64(0), fullCount)

	cql = "cql.allRecords=1"
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, fullCount, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  1,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, int64(2), fullCount)

	// has_internal_note=true selects requests that have a note (and round-trips its value)
	cql = `requester_req_id_exact = REQ-123 and has_internal_note=true`
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, _, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	if assert.Len(t, list, 1) {
		assert.Equal(t, prIds[0], list[0].ID)
		assert.Equal(t, "staff only", list[0].InternalNote.String)
	}

	// has_internal_note=false selects requests without one
	cql = `requester_req_id_exact = REQ-123 and has_internal_note=false`
	pgcql, err = pr_db.ParsePatronRequestsCql(cql)
	assert.NoError(t, err)
	list, _, err = prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{
		Limit:  10,
		Offset: 0,
	}, pgcql)
	assert.NoError(t, err)
	if assert.Len(t, list, 1) {
		assert.Equal(t, prIds[1], list[0].ID)
	}

	for _, prId := range prIds {
		err = prRepo.DeletePatronRequest(appCtx, prId)
		assert.NoError(t, err)
	}
}
