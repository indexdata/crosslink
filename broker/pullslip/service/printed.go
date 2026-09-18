package psservice

import (
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
)

// QueuePullslipPrinted records successful output through normal exclusive request
// actions. Only the supplied PDF's requests are considered; unsupported states
// are skipped and eligibility is checked again when the action runs.
func QueuePullslipPrinted(bus events.EventBus, requests []pr_db.PatronRequest, parentID *string, batch *events.BatchActionData) error {
	mappingService := &prservice.ActionMappingService{}
	action := prservice.LenderActionPullslipPrinted
	var failures []error
	for _, request := range requests {
		if request.Side != prservice.SideLending {
			continue
		}
		mapping, err := mappingService.GetActionMapping(request.IllRequest)
		if err != nil {
			failures = append(failures, fmt.Errorf("request %s: %w", request.ID, err))
			continue
		}
		if !mapping.IsActionSupported(request, action) {
			continue
		}
		data := events.EventData{CommonEventData: events.CommonEventData{Action: &action}}
		if batch != nil {
			child := *batch
			data.BatchActionData = &child
		}
		if _, err := bus.CreateTask(request.ID, events.EventNameInvokeBackgroundAction, data, events.EventDomainPatronRequest, parentID, events.SignalConsumers); err != nil {
			failures = append(failures, fmt.Errorf("queue pullslip-printed for %s: %w", request.ID, err))
		}
	}
	return errors.Join(failures...)
}
