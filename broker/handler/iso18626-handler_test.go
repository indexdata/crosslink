package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/indexdata/crosslink/directory"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

func TestGetSupplierSymbol(t *testing.T) {
	header := &iso18626.Header{
		SupplyingAgencyId: iso18626.TypeAgencyId{
			AgencyIdType: iso18626.TypeSchemeValuePair{
				Text: "ISIL",
			},
			AgencyIdValue: "12345",
		},
	}
	symbol := getSupplierSymbol(header)
	assert.Equal(t, "ISIL:12345", symbol)
	header.SupplyingAgencyId.AgencyIdType.Text = ""
	symbol = getSupplierSymbol(header)
	assert.Equal(t, "", symbol)
	header.SupplyingAgencyId.AgencyIdType.Text = "ISIL"
	header.SupplyingAgencyId.AgencyIdValue = ""
	symbol = getSupplierSymbol(header)
	assert.Equal(t, "", symbol)
}

func TestApplyRequesterShimError(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(mocks.MockIllRepositoryError)
	eventData := events.EventData{}
	err := applyRequesterShim(appCtx, mockRepo, "1", iso18626.NewISO18626Message(), &eventData, nil)
	assert.Equal(t, "DB error", err.Error())
}

func TestApplyRequesterShim(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(mocks.MockIllRepositorySuccess)
	eventData := events.EventData{}
	err := applyRequesterShim(appCtx, mockRepo, "1", iso18626.NewISO18626Message(), &eventData, nil)
	assert.NoError(t, err, "should not have DB error")
	assert.NotNil(t, eventData.IncomingMessage)
	assert.NotNil(t, eventData.CustomData[ORIGINAL_INCOMING_MESSAGE])
}

func TestApplyRequesterShimAlma(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(MockIllRepositorySuccessAlma)
	eventData := events.EventData{}
	message := iso18626.NewISO18626Message()
	message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "BROKER",
			},
		},
		Action: iso18626.TypeActionNotification,
		Note:   "ReJeCT",
	}
	err := applyRequesterShim(appCtx, mockRepo, "1", message, &eventData, &ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP1"})
	assert.NoError(t, err, "should not have DB error")
	assert.NotNil(t, eventData.IncomingMessage)
	assert.Equal(t, "SUP1", eventData.IncomingMessage.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
	assert.NotNil(t, eventData.CustomData[ORIGINAL_INCOMING_MESSAGE])
	assert.Equal(t, "BROKER", eventData.CustomData[ORIGINAL_INCOMING_MESSAGE].(*iso18626.ISO18626Message).RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
}

func TestGetRequesterMessageSupplierBrokerSymbolNoSelectedSupplier(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(MockIllRepositoryNoSelectedSupplier)

	supplier, err := getRequesterMessageSupplier(appCtx, mockRepo, "ill-1", brokerSymbol)

	assert.NoError(t, err)
	assert.Nil(t, supplier)
}

func TestGetRequesterMessageSupplierPeerSymbolRequiresSelectedSupplier(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(MockIllRepositoryNoSelectedSupplier)

	supplier, err := getRequesterMessageSupplier(appCtx, mockRepo, "ill-1", "ISIL:SUP1")

	assert.Nil(t, supplier)
	assert.True(t, errors.Is(err, ErrSupplierNotFoundOrInvalid))
}

func TestGetRequesterMessageSupplierBrokerSymbolUsesSelectedSupplierWhenPresent(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(mocks.MockIllRepositorySuccess)

	supplier, err := getRequesterMessageSupplier(appCtx, mockRepo, "ill-1", brokerSymbol)

	assert.NoError(t, err)
	if assert.NotNil(t, supplier) {
		assert.Equal(t, "ISIL:SUP", supplier.SupplierSymbol)
	}
}

func TestApplyRequesterShimAlmaWithoutSupplierDoesNotTurnRejectIntoCancel(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(MockIllRepositorySuccessAlma)
	eventData := events.EventData{}
	message := iso18626.NewISO18626Message()
	message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "BROKER",
			},
		},
		Action: iso18626.TypeActionNotification,
		Note:   "ReJeCT",
	}

	err := applyRequesterShim(appCtx, mockRepo, "1", message, &eventData, nil)

	assert.NoError(t, err, "should not have DB error")
	assert.NotNil(t, eventData.IncomingMessage)
	assert.Equal(t, iso18626.TypeActionNotification, eventData.IncomingMessage.RequestingAgencyMessage.Action)
	assert.Equal(t, "BROKER", eventData.IncomingMessage.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
}

type MockIllRepositorySuccessAlma struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockIllRepositorySuccessAlma) GetPeerById(ctx common.ExtendedContext, id string) (ill_db.Peer, error) {
	return ill_db.Peer{
		ID:     id,
		Vendor: string(directory.Alma),
	}, nil
}

type MockIllRepositoryNoSelectedSupplier struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockIllRepositoryNoSelectedSupplier) GetSelectedSupplierForIllTransaction(ctx common.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, pgx.ErrNoRows
}

// mockDuplicateCheckRepo overrides FindDuplicateIllTransaction to return
// configurable results for duplicate-check testing.
type mockDuplicateCheckRepo struct {
	mocks.MockIllRepositorySuccess
	duplicateId string
	err         error
	called      bool
	params      ill_db.FindDuplicateIllTransactionParams
}

func (r *mockDuplicateCheckRepo) FindDuplicateIllTransaction(ctx common.ExtendedContext, params ill_db.FindDuplicateIllTransactionParams) (string, error) {
	r.called = true
	r.params = params
	return r.duplicateId, r.err
}

func TestCheckDuplicateRequest(t *testing.T) {
	window1 := 1
	window0 := 0
	windowNeg := -1

	baseRequest := &iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: "rec-1",
			Title:                  "Test Title",
		},
		ServiceInfo: &iso18626.ServiceInfo{
			ServiceType: iso18626.TypeServiceTypeLoan,
		},
		PatronInfo: &iso18626.PatronInfo{
			PatronId: "patron-1",
		},
	}

	isbnRequest := &iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			BibliographicItemId: []iso18626.BibliographicItemId{
				{
					BibliographicItemIdentifier:     "978-1234",
					BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: "ISBN"},
				},
			},
		},
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeCopy},
		PatronInfo:  &iso18626.PatronInfo{PatronId: "patron-2"},
	}

	tests := []struct {
		name           string
		request        *iso18626.Request
		peer           ill_db.Peer
		duplicateId    string
		repoErr        error
		wantErr        error
		wantRepoCalled bool
		wantPatronId   string
		wantWindowHrs  int32
		wantIdentifier string
		wantIsbn       string
		wantIssn       string
		wantTitle      string
		wantSvcType    string
	}{
		{
			name:           "no DuplicateCheckWindowHours configured - skips check",
			request:        baseRequest,
			peer:           ill_db.Peer{},
			wantErr:        nil,
			wantRepoCalled: false,
		},
		{
			name:           "window is zero - skips check",
			request:        baseRequest,
			peer:           ill_db.Peer{CustomData: directory.Entry{DuplicateCheckWindowHours: &window0}},
			wantErr:        nil,
			wantRepoCalled: false,
		},
		{
			name:           "window is negative - skips check",
			request:        baseRequest,
			peer:           ill_db.Peer{CustomData: directory.Entry{DuplicateCheckWindowHours: &windowNeg}},
			wantErr:        nil,
			wantRepoCalled: false,
		},
		{
			name:           "db error - fails open, allows request through",
			request:        baseRequest,
			peer:           ill_db.Peer{CustomData: directory.Entry{DuplicateCheckWindowHours: &window1}},
			repoErr:        errors.New("db connection error"),
			wantErr:        nil,
			wantRepoCalled: true,
			wantPatronId:   "patron-1",
			wantWindowHrs:  1,
			wantIdentifier: "rec-1",
			wantTitle:      "Test Title",
			wantSvcType:    "Loan",
		},
		{
			name:           "no duplicate found (ErrNoRows) - not a duplicate",
			request:        baseRequest,
			peer:           ill_db.Peer{CustomData: directory.Entry{DuplicateCheckWindowHours: &window1}},
			repoErr:        pgx.ErrNoRows,
			wantErr:        nil,
			wantRepoCalled: true,
			wantPatronId:   "patron-1",
			wantWindowHrs:  1,
			wantIdentifier: "rec-1",
			wantTitle:      "Test Title",
			wantSvcType:    "Loan",
		},
		{
			name:           "duplicate found - returns ErrDuplicateRequest",
			request:        baseRequest,
			peer:           ill_db.Peer{CustomData: directory.Entry{DuplicateCheckWindowHours: &window1}},
			duplicateId:    "existing-tx-id",
			wantErr:        ErrDuplicateRequest,
			wantRepoCalled: true,
			wantPatronId:   "patron-1",
			wantWindowHrs:  1,
			wantIdentifier: "rec-1",
			wantTitle:      "Test Title",
			wantSvcType:    "Loan",
		},
		{
			name: "nil PatronInfo - skips duplicate check (can't verify same patron)",
			request: &iso18626.Request{
				BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "rec-1"},
				ServiceInfo:       &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
			},
			peer:           ill_db.Peer{CustomData: directory.Entry{DuplicateCheckWindowHours: &window1}},
			repoErr:        pgx.ErrNoRows,
			wantErr:        nil,
			wantRepoCalled: false,
		},
		{
			name:           "isbn passed as parameter to DB query",
			request:        isbnRequest,
			peer:           ill_db.Peer{CustomData: directory.Entry{DuplicateCheckWindowHours: &window1}},
			repoErr:        pgx.ErrNoRows,
			wantErr:        nil,
			wantRepoCalled: true,
			wantPatronId:   "patron-2",
			wantWindowHrs:  1,
			wantIsbn:       "978-1234",
			wantSvcType:    "Copy",
		},
		{
			name:           "duplicate found via isbn - returns ErrDuplicateRequest",
			request:        isbnRequest,
			peer:           ill_db.Peer{CustomData: directory.Entry{DuplicateCheckWindowHours: &window1}},
			duplicateId:    "existing-tx-isbn",
			wantErr:        ErrDuplicateRequest,
			wantRepoCalled: true,
			wantPatronId:   "patron-2",
			wantWindowHrs:  1,
			wantIsbn:       "978-1234",
			wantSvcType:    "Copy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
			mockRepo := &mockDuplicateCheckRepo{
				duplicateId: tt.duplicateId,
				err:         tt.repoErr,
			}
			err := checkDuplicateRequest(appCtx, tt.request, mockRepo, "ISIL:REQ1", tt.peer)
			assert.Equal(t, tt.wantErr, err)
			assert.Equal(t, tt.wantRepoCalled, mockRepo.called)
			if tt.wantRepoCalled {
				assert.Equal(t, "ISIL:REQ1", mockRepo.params.RequesterSymbol.String)
				assert.Equal(t, tt.wantPatronId, mockRepo.params.PatronID)
				assert.Equal(t, tt.wantWindowHrs, mockRepo.params.Hours)
				assert.Equal(t, tt.wantIdentifier, mockRepo.params.Identifier)
				assert.Equal(t, tt.wantIsbn, mockRepo.params.Isbn)
				assert.Equal(t, tt.wantIssn, mockRepo.params.Issn)
				assert.Equal(t, tt.wantTitle, mockRepo.params.Title)
				assert.Equal(t, tt.wantSvcType, mockRepo.params.ServiceType)
			}
		})
	}
}
