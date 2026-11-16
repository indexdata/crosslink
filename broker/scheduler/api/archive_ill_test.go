package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	testmocks "github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func (m *MockSchedRepo) QueueBatchActionRun(_ common.ExtendedContext, task sched_db.ScheduledTask) error {
	return m.Called(task).Error(0)
}

func consortiumHandler(repo *MockSchedRepo) SchedulerApiHandler {
	resolver := tenant.NewResolver().WithTenantToSymbol(testSymbol).WithIllRepo(new(testmocks.MockIllRepositorySuccess))
	return NewSchedulerApiHandler(10, repo, nil, resolver, nil)
}

func TestTransactionArchiveMutationAuthorization(t *testing.T) {
	for _, operation := range []string{"create", "update", "enable", "disable", "delete", "run"} {
		for _, allowed := range []bool{false, true} {
			name := operation + "/denied"
			if allowed {
				name = operation + "/allowed"
			}
			t.Run(name, func(t *testing.T) {
				repo := new(MockSchedRepo)
				h := consortiumHandler(repo)
				task := scheduledTaskFixture("ill-archive")
				task.ActionData.BatchActionData.ActionName = string(schedoapi.ArchiveIllTransactions)
				task.ActionData.BatchActionData.Selector = "last_supplier_status==LoanCompleted"
				task.ActionData.CustomData = map[string]any{"interval": "24h"}
				if operation != "create" {
					repo.On("GetScheduledTaskByIdForUpdate", task.ID, testOwnerScope).Return(task, nil).Once()
				}
				if allowed {
					switch operation {
					case "create", "update", "enable", "disable":
						repo.On("SaveScheduledTask", mock.Anything).Return(func(p sched_db.SaveScheduledTaskParams) sched_db.ScheduledTask { return sched_db.ScheduledTask(p) }, nil).Once()
					case "delete":
						repo.On("HasActiveBatchActionEvents", task.ID).Return(false, nil).Once()
						repo.On("DeleteBatchActionEvents", task.ID).Return(nil).Once()
						repo.On("DeleteScheduledTask", task.ID, testOwnerScope).Return(nil).Once()
					case "run":
						repo.On("QueueBatchActionRun", task).Return(nil).Once()
					}
				}
				body := `{"title":"Archive","schedule":"FREQ=DAILY","actionName":"archive-ill-transactions","batchQuery":"last_supplier_status==LoanCompleted","actionParams":{"interval":"24h"}}`
				if operation == "update" {
					body = `{"title":"Archive","schedule":"FREQ=DAILY","batchQuery":"last_supplier_status==LoanCompleted"}`
				}
				req := newReq(http.MethodPost, body)
				req.URL.Path = "/broker/batch_actions"
				req.Header.Set(tenant.OkapiTenantHeader, "test")
				if allowed {
					req.Header.Set("X-Okapi-Permissions", `["broker.consortium.all"]`)
				}
				rr := httptest.NewRecorder()
				switch operation {
				case "create":
					h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})
				case "update":
					h.PutBatchActionsId(rr, req, task.ID, schedoapi.PutBatchActionsIdParams{Symbol: symPtr(testSymbol)})
				case "enable":
					h.PostBatchActionsIdEnable(rr, req, task.ID, schedoapi.PostBatchActionsIdEnableParams{Symbol: symPtr(testSymbol)})
				case "disable":
					h.PostBatchActionsIdDisable(rr, req, task.ID, schedoapi.PostBatchActionsIdDisableParams{Symbol: symPtr(testSymbol)})
				case "delete":
					h.DeleteBatchActionsId(rr, req, task.ID, schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})
				case "run":
					h.PostBatchActionsIdRun(rr, req, task.ID, schedoapi.PostBatchActionsIdRunParams{Symbol: symPtr(testSymbol)})
				}
				if !allowed {
					assert.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
				} else {
					assert.Less(t, rr.Code, 300, rr.Body.String())
				}
				repo.AssertExpectations(t)
			})
		}
	}
}

func TestManualBatchRunQueuesWithoutCheckingActiveEvents(t *testing.T) {
	for _, action := range []string{string(schedoapi.RequestAging), string(schedoapi.EmailPullslips)} {
		for _, failQueue := range []bool{false, true} {
			name := action + "/queued"
			var queueErr error
			wantStatus := http.StatusAccepted
			if failQueue {
				name = action + "/queue fails"
				queueErr = errors.New("queue failed")
				wantStatus = http.StatusInternalServerError
			}
			t.Run(name, func(t *testing.T) {
				repo := new(MockSchedRepo)
				h := newHandler(repo)
				task := scheduledTaskFixture("active")
				task.ActionData.BatchActionData.ActionName = action
				repo.On("GetScheduledTaskByIdForUpdate", task.ID, testOwnerScope).Return(task, nil).Once()
				repo.On("QueueBatchActionRun", task).Return(queueErr).Once()
				rr := httptest.NewRecorder()
				h.PostBatchActionsIdRun(rr, newReq(http.MethodPost, ""), task.ID, schedoapi.PostBatchActionsIdRunParams{Symbol: symPtr(testSymbol)})
				assert.Equal(t, wantStatus, rr.Code, rr.Body.String())
				repo.AssertNotCalled(t, "HasActiveBatchActionEvents", mock.Anything)
				repo.AssertExpectations(t)
			})
		}
	}
}
