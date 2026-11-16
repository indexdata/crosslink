package prservice

import (
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenewalRoundTrip(t *testing.T) {
	for _, serviceType := range []iso18626.TypeServiceType{iso18626.TypeServiceTypeLoan, iso18626.TypeServiceTypeCopyOrLoan} {
		for _, origin := range []pr_db.PatronRequestState{BorrowerStateReceived, BorrowerStateRenewed, BorrowerStateOverdue} {
			for _, dated := range []bool{false, true} {
				for _, accept := range []bool{false, true} {
					name := string(serviceType) + "/" + string(origin)
					if dated {
						name += "/dated"
					} else {
						name += "/open-ended"
					}
					action := LenderActionRejectRenewal
					if accept {
						action = LenderActionAcceptRenewal
					}
					t.Run(name+"/"+string(action), func(t *testing.T) {
						borrower, supplier := testLoan(), testLoan()
						borrower.Side, borrower.State, supplier.State = SideBorrowing, origin, origin
						borrower.IllRequest.ServiceInfo.ServiceType = serviceType
						supplier.IllRequest.ServiceInfo.ServiceType = serviceType
						if dated {
							due := time.Now().UTC().AddDate(0, 0, 7)
							if origin == BorrowerStateOverdue {
								due = due.AddDate(0, 0, -14)
							}
							borrower.DueAt = pgtype.Timestamptz{Time: due, Valid: true}
							supplier.DueAt = borrower.DueAt
						}
						if origin == BorrowerStateOverdue {
							borrower.IllResponse.StatusInfo.Status = iso18626.TypeStatusOverdue
							supplier.IllResponse.StatusInfo.Status = iso18626.TypeStatusOverdue
						}
						borrower.IllResponse.StatusInfo.DueDate = isoLoanDate(borrower.DueAt)
						supplier.IllResponse.StatusInfo.DueDate = isoLoanDate(supplier.DueAt)
						borrowRepo, supplierRepo := &MockPrRepo{savedPr: borrower}, &MockPrRepo{savedPr: supplier}
						borrowSender := &MockIso18626Handler{failRequestingAgencyMessage: true}
						creator := new(MockLmsCreator)
						creator.On("GetAdapter", borrower.RequesterSymbol.String).Return(&lms.LmsAdapterManual{}, nil)
						creator.On("GetAdapter", supplier.SupplierSymbol.String).Return(&lms.LmsAdapterManual{}, nil)
						borrowSvc := CreatePatronRequestActionService(borrowRepo, new(IllRepoMock), new(MockEventBus), borrowSender, creator, nil, nil, nil)
						renew := BorrowerActionRenew
						event := events.Event{ID: "renew", PatronRequestID: borrower.ID, EventData: events.EventData{
							CommonEventData: events.CommonEventData{Action: &renew}, CustomData: map[string]any{"note": "please renew"},
						}}
						status, _ := borrowSvc.handleInvokeAction(appCtx, event)
						require.NotEqual(t, events.EventStatusSuccess, status)
						assert.Equal(t, origin, borrowRepo.savedPr.State)
						assert.Equal(t, borrower.DueAt, borrowRepo.savedPr.DueAt)
						borrowSender.failRequestingAgencyMessage = false
						status, _ = borrowSvc.handleInvokeAction(appCtx, event)
						require.Equal(t, events.EventStatusSuccess, status)
						assert.Equal(t, BorrowerStateRenewalPending, borrowRepo.savedPr.State)
						require.NotNil(t, borrowSender.lastRequestingAgencyMessage)
						assert.Equal(t, "please renew", borrowSender.lastRequestingAgencyMessage.Note)

						supplierHandler := CreatePatronRequestMessageHandler(supplierRepo, nil, nil, nil)
						status, _, err := supplierHandler.handleRequestingAgencyMessage(appCtx, *borrowSender.lastRequestingAgencyMessage, supplier)
						require.NoError(t, err)
						require.Equal(t, events.EventStatusSuccess, status)
						assert.Equal(t, BorrowerStateRenewalPending, supplierRepo.savedPr.State)
						assert.True(t, supplierRepo.savedPr.NeedsAttention)
						// Duplicate requests leave the renewal pending.
						status, _, err = supplierHandler.handleRequestingAgencyMessage(appCtx, *borrowSender.lastRequestingAgencyMessage, supplierRepo.savedPr)
						require.NoError(t, err)
						require.Equal(t, events.EventStatusSuccess, status)
						assert.Equal(t, BorrowerStateRenewalPending, supplierRepo.savedPr.State)

						// A new service instance uses only the persisted pending state.
						supplierSender := &MockIso18626Handler{failSupplyingAgencyMessage: true}
						supplierSvc := CreatePatronRequestActionService(supplierRepo, new(IllRepoMock), new(MockEventBus), supplierSender, creator, nil, nil, nil)
						decision := events.Event{ID: "decision", PatronRequestID: supplier.ID, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}}
						status, _ = supplierSvc.handleInvokeAction(appCtx, decision)
						require.NotEqual(t, events.EventStatusSuccess, status)
						assert.Equal(t, BorrowerStateRenewalPending, supplierRepo.savedPr.State)
						assert.Equal(t, supplier.DueAt, supplierRepo.savedPr.DueAt)
						supplierSender.failSupplyingAgencyMessage = false
						status, _ = supplierSvc.handleInvokeAction(appCtx, decision)
						require.Equal(t, events.EventStatusSuccess, status)
						wantState, wantDue, wantStatus := BorrowerStateRenewalRejected, supplier.DueAt, iso18626.TypeStatusLoaned
						if accept {
							wantState, wantDue = BorrowerStateRenewed, pgtype.Timestamptz{}
						} else if origin == BorrowerStateOverdue {
							wantStatus = iso18626.TypeStatusOverdue
						}
						assert.Equal(t, wantState, supplierRepo.savedPr.State)
						assert.Equal(t, wantDue, supplierRepo.savedPr.DueAt)
						require.NotNil(t, supplierSender.lastSupplyingAgencyMessage)
						assert.Equal(t, wantStatus, supplierSender.lastSupplyingAgencyMessage.StatusInfo.Status)

						borrowHandler := CreatePatronRequestMessageHandler(borrowRepo, nil, nil, nil)
						runner := new(MockAutoActionRunner)
						borrowHandler.SetAutoActionRunner(runner)
						status, _, err = borrowHandler.handleSupplyingAgencyMessage(appCtx, *supplierSender.lastSupplyingAgencyMessage, borrowRepo.savedPr)
						require.NoError(t, err)
						require.Equal(t, events.EventStatusSuccess, status)
						assert.Equal(t, wantState, borrowRepo.savedPr.State)
						assert.Equal(t, wantDue, borrowRepo.savedPr.DueAt)
						assert.Equal(t, wantStatus, borrowRepo.savedPr.IllResponse.StatusInfo.Status)
						assert.False(t, borrowRepo.savedPr.NeedsAttention)
						assert.Equal(t, 1, runner.callCount)
						assert.Equal(t, accept, mustActionMapping(t).IsActionSupported(borrowRepo.savedPr, BorrowerActionRenew))
						if !accept {
							status, _, err = supplierHandler.handleRequestingAgencyMessage(appCtx, *borrowSender.lastRequestingAgencyMessage, supplierRepo.savedPr)
							require.Error(t, err)
							assert.Equal(t, events.EventStatusProblem, status)
							assert.Equal(t, LenderStateRenewalRejected, supplierRepo.savedPr.State)
						}
					})
				}
			}
		}
	}
}

func TestRejectedLoanOverdueAndReturn(t *testing.T) {
	for _, loanStatus := range []iso18626.TypeStatus{iso18626.TypeStatusLoaned, iso18626.TypeStatusOverdue} {
		t.Run(string(loanStatus), func(t *testing.T) {
			supplier := testLoan()
			supplier.State = LenderStateRenewalRejected
			supplier.IllResponse.StatusInfo.Status = loanStatus
			supplier.DueAt = pgtype.Timestamptz{Time: time.Now().UTC().Add(-time.Hour), Valid: true}
			supplier.IllResponse.StatusInfo.DueDate = isoLoanDate(supplier.DueAt)
			supplierRepo := &MockPrRepo{savedPr: supplier}
			sender := new(MockIso18626Handler)
			creator := new(MockLmsCreator)
			creator.On("GetAdapter", supplier.SupplierSymbol.String).Return(&lms.LmsAdapterManual{}, nil)
			svc := CreatePatronRequestActionService(supplierRepo, new(IllRepoMock), new(MockEventBus), sender, creator, nil, nil, nil)
			action := LenderActionOverdue
			event := events.Event{ID: "overdue", PatronRequestID: supplier.ID, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}}
			sender.failSupplyingAgencyMessage = true
			status, _ := svc.handleInvokeAction(appCtx, event)
			require.NotEqual(t, events.EventStatusSuccess, status)
			assert.Equal(t, LenderStateRenewalRejected, supplierRepo.savedPr.State)
			assert.Equal(t, supplier.IllResponse, supplierRepo.savedPr.IllResponse)
			sender.failSupplyingAgencyMessage = false
			status, _ = svc.handleInvokeAction(appCtx, event)
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, LenderStateOverdue, supplierRepo.savedPr.State)
			assert.Equal(t, supplier.DueAt, supplierRepo.savedPr.DueAt)
			require.NotNil(t, sender.lastSupplyingAgencyMessage)
			assert.Equal(t, iso18626.TypeStatusOverdue, sender.lastSupplyingAgencyMessage.StatusInfo.Status)
			overdueMessage := *sender.lastSupplyingAgencyMessage
			sender.lastSupplyingAgencyMessage = nil
			status, _ = svc.handleInvokeAction(appCtx, event)
			require.NotEqual(t, events.EventStatusSuccess, status)
			assert.Nil(t, sender.lastSupplyingAgencyMessage, "the OVERDUE state no longer supports the overdue action")
			assert.Equal(t, LenderStateOverdue, supplierRepo.savedPr.State)

			borrower := supplier
			borrower.Side = SideBorrowing
			borrowRepo := &MockPrRepo{savedPr: borrower}
			borrowHandler := CreatePatronRequestMessageHandler(borrowRepo, nil, nil, nil)
			status, _, err := borrowHandler.handleSupplyingAgencyMessage(appCtx, overdueMessage, borrower)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, BorrowerStateOverdue, borrowRepo.savedPr.State)
			assert.Equal(t, borrower.DueAt, borrowRepo.savedPr.DueAt)
			assert.Equal(t, iso18626.TypeStatusOverdue, borrowRepo.savedPr.IllResponse.StatusInfo.Status)
			assert.True(t, mustActionMapping(t).IsActionSupported(borrowRepo.savedPr, BorrowerActionRenew))

			supplierHandler := CreatePatronRequestMessageHandler(supplierRepo, nil, nil, nil)
			status, _, err = supplierHandler.handleRequestingAgencyMessage(appCtx, iso18626.RequestingAgencyMessage{Action: iso18626.TypeActionShippedReturn}, supplierRepo.savedPr)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, LenderStateShippedReturn, supplierRepo.savedPr.State)
		})
	}
}

func TestEarlyRenewalUnavailableForCopiesAndOtherLoanStates(t *testing.T) {
	for _, serviceType := range []iso18626.TypeServiceType{iso18626.TypeServiceTypeLoan, iso18626.TypeServiceTypeCopyOrLoan, iso18626.TypeServiceTypeCopy} {
		pr := testLoan()
		pr.Side = SideBorrowing
		pr.IllRequest.ServiceInfo.ServiceType = serviceType
		mapping, err := actionMappingService.GetActionMapping(pr.IllRequest)
		require.NoError(t, err)
		for _, state := range []pr_db.PatronRequestState{BorrowerStateReceived, BorrowerStateRenewed, BorrowerStateOverdue, BorrowerStateRenewalPending, BorrowerStateRenewalRejected, BorrowerStateShipped, BorrowerStateRecalled, BorrowerStateShippedReturned, BorrowerStateCompleted} {
			pr.State = state
			want := serviceType != iso18626.TypeServiceTypeCopy && (state == BorrowerStateReceived || state == BorrowerStateRenewed || state == BorrowerStateOverdue)
			assert.Equal(t, want, mapping.IsActionSupported(pr, BorrowerActionRenew), "%s/%s", serviceType, state)
		}
	}
}
