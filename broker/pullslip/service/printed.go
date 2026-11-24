package psservice

import (
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
)

// QueuePullslipPrinted records successful output through normal exclusive request
// actions. Existing batch children are reused, and new tasks are only queued
// for eligible requests from the supplied PDF. The action rechecks eligibility.
func QueuePullslipPrinted(bus events.EventBus, requests []pr_db.PatronRequest, parentID *string, batch *events.BatchActionData) error {
	mappingService := &prservice.ActionMappingService{}
	action := prservice.LenderActionPullslipPrinted
	var failures []error
	for _, request := range requests {
		data := events.EventData{CommonEventData: events.CommonEventData{Action: &action}}
		if batch != nil {
			child := *batch
			data.BatchActionData = &child
		}
		if err := events.CreateBackgroundActionTaskIfEligible(bus, request.ID, data, parentID, func() (bool, error) {
			if request.Side != prservice.SideLending {
				return false, nil
			}
			mapping, err := mappingService.GetActionMapping(request.IllRequest)
			if err != nil {
				return false, fmt.Errorf("request %s: %w", request.ID, err)
			}
			return mapping.IsActionSupported(request, action), nil
		}); err != nil {
			failures = append(failures, fmt.Errorf("queue pullslip-printed for %s: %w", request.ID, err))
		}
	}
	return errors.Join(failures...)
}
