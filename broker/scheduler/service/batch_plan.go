package sched_service

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/repo"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
)

// visitBatchPlan freezes the selection before queueing and streams committed plan pages.
func (s *BatchActionService) visitBatchPlan(ctx common.ExtendedContext, runID string, selectEntries func() ([]sched_db.BatchActionPlanEntry, error), visit func(sched_db.BatchActionPlanEntry)) error {
	if repo.BatchAttemptFrom(ctx).ID == "" {
		entries, err := selectEntries()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			visit(entry)
		}
		return nil
	}

	saved, err := s.schedRepo.IsBatchActionPlanSaved(ctx, runID)
	if err != nil {
		return err
	}
	if !saved {
		entries, err := selectEntries()
		if err != nil {
			return err
		}
		if err := s.schedRepo.SaveBatchActionPlan(ctx, runID, entries); err != nil {
			return err
		}
	}
	return s.visitSavedBatchPlan(ctx, runID, visit)
}

func (s *BatchActionService) visitSavedBatchPlan(ctx common.ExtendedContext, runID string, visit func(sched_db.BatchActionPlanEntry)) error {
	var after int64
	for {
		page, err := s.schedRepo.GetBatchActionPlanEntries(ctx, runID, after)
		if err != nil {
			return err
		}
		for _, entry := range page {
			visit(entry)
		}
		if len(page) < sched_db.BatchActionPlanPageSize {
			return nil
		}
		after = page[len(page)-1].Position
	}
}

func (s *BatchActionService) queuePlannedAction(ctx common.ExtendedContext, event events.Event, entry sched_db.BatchActionPlanEntry, params map[string]any) error {
	batch := *event.EventData.BatchActionData
	action := pr_db.PatronRequestAction(entry.Action)
	data := events.EventData{
		CommonEventData: events.CommonEventData{Action: &action, BatchActionData: &batch},
		CustomData:      params,
	}
	_, err := events.WithWorkerContext(s.eventBus, ctx).CreateTask(entry.RequestID, events.EventNameInvokeBackgroundAction,
		data, events.EventDomainPatronRequest, &event.ID, events.SignalConsumers)
	return err
}
