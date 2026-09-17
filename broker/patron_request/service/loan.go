package prservice

import (
	"fmt"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
)

func loanActionError(ctx common.ExtendedContext, pr pr_db.PatronRequest, err error) actionExecutionResult {
	status, result := logActionErrorAndReturnResult(ctx, err.Error(), err)
	return actionExecutionResult{status: status, result: result, pr: pr}
}

func validateLoanDueDateParam(data map[string]any) error {
	if supplied, ok := data["dueDate"]; ok {
		value, valid := supplied.(string)
		if !valid || strings.TrimSpace(value) == "" {
			return fmt.Errorf("supplied dueDate must be a non-empty date or RFC3339 timestamp")
		}
	}
	return nil
}

func supplierLocation(entry dirapi.Entry) (*time.Location, error) {
	if entry.TimeZone == nil || *entry.TimeZone == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(*entry.TimeZone)
}

// Dates entered without a time mean the end of the supplier's calendar day.
func parseLoanDate(value string, entry dirapi.Entry) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if len(value) == len(time.DateOnly) {
		loc, err := supplierLocation(entry)
		if err != nil {
			return nil, err
		}
		date, err := time.ParseInLocation(time.DateOnly, value, loc)
		if err != nil || date.IsZero() || date.Year() < 1 {
			return nil, fmt.Errorf("invalid dueDate %q", value)
		}
		date = time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 0, loc)
		return &date, nil
	}
	date, err := time.Parse(time.RFC3339, value)
	if err != nil || date.IsZero() || date.Year() < 1 {
		return nil, fmt.Errorf("dueDate must be a valid date or RFC3339 timestamp")
	}
	return &date, nil
}

func defaultLoanDate(entry dirapi.Entry, now time.Time) (*time.Time, error) {
	if entry.IllConfig == nil {
		return nil, nil
	}
	days, err := entry.IllConfig.DefaultLoanPeriod.Get()
	if err != nil {
		return nil, nil
	}
	if days < 1 {
		return nil, fmt.Errorf("defaultLoanPeriod must be a positive integer")
	}
	loc, err := supplierLocation(entry)
	if err != nil {
		return nil, err
	}
	day := now.In(loc).AddDate(0, 0, int(days))
	date := time.Date(day.Year(), day.Month(), day.Day(), 23, 59, 59, 0, loc)
	if date.Year() > 9999 {
		return nil, fmt.Errorf("defaultLoanPeriod produces an unrepresentable ISO due date")
	}
	return &date, nil
}

// resolveLoanDueDate selects the earliest checkout date, then the manual date,
// then the supplier's default. The source is returned for the shipment audit.
// A nil date means an open-ended loan.
func resolveLoanDueDate(items []pr_db.Item, manualDue *time.Time, entry dirapi.Entry, now time.Time) (*time.Time, string, error) {
	var due *time.Time
	for _, item := range items {
		if item.LmsDueDate.Valid && !item.LmsDueDate.Time.IsZero() && (due == nil || item.LmsDueDate.Time.Before(*due)) {
			date := item.LmsDueDate.Time
			due = &date
		}
	}
	if due != nil {
		return due, "LMS checkout", nil
	}
	if manualDue != nil {
		return manualDue, "ship.dueDate", nil
	}
	due, err := defaultLoanDate(entry, now)
	if err != nil || due == nil {
		return nil, "", err
	}
	return due, "illConfig.defaultLoanPeriod", nil
}

func (a *PatronRequestActionService) supplierLoanEntry(ctx common.ExtendedContext, pr pr_db.PatronRequest) (dirapi.Entry, error) {
	peers, _, err := a.illRepo.GetCachedPeersBySymbols(ctx, []string{pr.SupplierSymbol.String}, a.directoryLookupAdapter)
	if err != nil {
		return dirapi.Entry{}, err
	}
	if len(peers) == 0 {
		return dirapi.Entry{}, fmt.Errorf("supplier directory entry not found")
	}
	return peers[0].CustomData, nil
}

func (a *PatronRequestActionService) recordItemLmsStatus(ctx common.ExtendedContext, item pr_db.Item, status pr_db.LmsStatus, performed bool, due *time.Time) error {
	if performed {
		date := item.LmsDueDate
		if due != nil && !due.IsZero() {
			date = pgtype.Timestamptz{Time: *due, Valid: true}
		}
		if err := a.prRepo.SetItemLmsStatus(ctx, pr_db.SetItemLmsStatusParams{ID: item.ID, LmsStatus: status, LmsDueDate: date}); err != nil {
			return err
		}
	}
	return nil
}

// Item edits invalidate the resolved shipment date atomically. Delivery-only
// retries never call this helper and keep the previously resolved date.
func (a *PatronRequestActionService) editSupplierItems(ctx common.ExtendedContext, pr pr_db.PatronRequest, edit func(pr_db.PrRepo) error) (pr_db.PatronRequest, error) {
	updated := pr
	err := a.prRepo.WithTxFunc(ctx, func(repo pr_db.PrRepo) error {
		if err := edit(repo); err != nil {
			return err
		}
		if !pr.DueAt.Valid && pr.IllResponse.StatusInfo.DueDate == nil {
			return nil
		}
		updated.DueAt = pgtype.Timestamptz{}
		updated.IllResponse.StatusInfo.DueDate = nil
		var err error
		updated, err = repo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(updated))
		return err
	})
	if err != nil {
		return pr, err
	}
	return updated, nil
}

func isoLoanDate(date pgtype.Timestamptz) *utils.XSDDateTime {
	if !date.Valid {
		return nil
	}
	return &utils.XSDDateTime{Time: date.Time}
}

// Loan updates refresh message info and status without discarding shipment and return details.
// Only an accepted renewal changes the canonical date; other updates retain it.
func setLoanMessage(sam iso18626.SupplyingAgencyMessage, pr *pr_db.PatronRequest) {
	pr.IllResponse.MessageInfo = sam.MessageInfo
	pr.IllResponse.StatusInfo.Status = sam.StatusInfo.Status
	pr.IllResponse.StatusInfo.LastChange = sam.StatusInfo.LastChange
	pr.IllResponse.StatusInfo.DueDate = isoLoanDate(pr.DueAt)
}

func (a *PatronRequestActionService) overdueLenderRequest(ctx common.ExtendedContext, eventID string, pr pr_db.PatronRequest) actionExecutionResult {
	// The action dispatcher enforces availability using the request's state model.
	if pr.Side != SideLending ||
		!pr.DueAt.Valid || !pr.DueAt.Time.Before(time.Now()) || pr.IllResponse.StatusInfo.Status == iso18626.TypeStatusCopyCompleted ||
		(pr.IllRequest.ServiceInfo != nil && pr.IllRequest.ServiceInfo.ServiceType == iso18626.TypeServiceTypeCopy) {
		return loanActionError(ctx, pr, fmt.Errorf("loan is not eligible for overdue"))
	}
	status, result, err := a.messageSender.sendSupplyingAgencyMessage(ctx, eventID, pr,
		iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange},
		iso18626.StatusInfo{Status: iso18626.TypeStatusOverdue, DueDate: isoLoanDate(pr.DueAt)}, nil)
	execution := actionResultFromIllSend(ctx, status, result, err, pr)
	if execution.status == events.EventStatusSuccess {
		setLoanMessage(*result.OutgoingMessage.SupplyingAgencyMessage, &execution.pr)
	}
	return execution
}

func (a *PatronRequestActionService) renewalLenderRequest(ctx common.ExtendedContext, eventID string, pr pr_db.PatronRequest, actionCustomData map[string]any, accept bool) actionExecutionResult {
	if accept {
		if err := validateLoanDueDateParam(actionCustomData); err != nil {
			return loanActionError(ctx, pr, err)
		}
	}
	var params actionParams
	if err := common.MapToStruct(actionCustomData, &params); err != nil {
		return loanActionError(ctx, pr, err)
	}
	answer, status := iso18626.TypeYesNoN, iso18626.TypeStatusOverdue
	due := pr.DueAt
	if accept {
		due = pgtype.Timestamptz{}
		if params.DueDate != "" {
			entry, err := a.supplierLoanEntry(ctx, pr)
			if err != nil {
				return loanActionError(ctx, pr, err)
			}
			date, err := parseLoanDate(params.DueDate, entry)
			if err != nil {
				return loanActionError(ctx, pr, err)
			}
			if date == nil || !date.After(time.Now()) {
				return loanActionError(ctx, pr, fmt.Errorf("supplied renewal dueDate must be in the future"))
			}
			due = pgtype.Timestamptz{Time: *date, Valid: true}
		}
		answer, status = iso18626.TypeYesNoY, iso18626.TypeStatusLoaned
	}
	sendStatus, result, err := a.messageSender.sendSupplyingAgencyMessage(ctx, eventID, pr,
		iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageRenewResponse, AnswerYesNo: &answer, Note: params.Note},
		iso18626.StatusInfo{Status: status, DueDate: isoLoanDate(due)}, nil)
	execution := actionResultFromIllSend(ctx, sendStatus, result, err, pr)
	if execution.status == events.EventStatusSuccess {
		if accept {
			execution.pr.DueAt = due
		}
		setLoanMessage(*result.OutgoingMessage.SupplyingAgencyMessage, &execution.pr)
	}
	return execution
}
