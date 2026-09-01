package importdb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	brokerepo "github.com/indexdata/crosslink/broker/repo"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgImportRepo struct {
	brokerepo.PgBaseRepo[ImportRepo]
	queries      *Queries
	prQueries    *pr_db.Queries
	illQueries   *ill_db.Queries
	schedQueries *sched_db.Queries
}

// WithTxFunc delegates transaction handling to PgBaseRepo.
func (r *PgImportRepo) WithTxFunc(ctx common.ExtendedContext, fn func(ImportRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// CreateWithPgBaseRepo creates a derived repo bound to the provided tx-aware base.
func (r *PgImportRepo) CreateWithPgBaseRepo(base *brokerepo.PgBaseRepo[ImportRepo]) ImportRepo {
	derived := new(PgImportRepo)
	derived.PgBaseRepo = *base
	derived.queries = r.queries
	derived.prQueries = r.prQueries
	derived.illQueries = r.illQueries
	derived.schedQueries = r.schedQueries
	return derived
}

func (r *PgImportRepo) withTxConn(ctx common.ExtendedContext, fn func(DBTX) error) error {
	return r.WithTxFunc(ctx, func(txRepo ImportRepo) error {
		txImportRepo, ok := txRepo.(*PgImportRepo)
		if !ok {
			return errors.New("unexpected import repo implementation")
		}
		return fn(txImportRepo.GetConnOrTx())
	})
}

func CreateImportRepo(pool *pgxpool.Pool) ImportRepo {
	r := new(PgImportRepo)
	r.Pool = pool
	r.queries = New()
	r.prQueries = pr_db.New()
	r.illQueries = ill_db.New()
	r.schedQueries = sched_db.New()
	return r
}

func (r *PgImportRepo) ImportPatronRequest(ctx common.ExtendedContext, bundle PatronRequestBundle, policy ConflictPolicy) (Result, error) {
	if err := policy.validate(); err != nil {
		return Result{}, err
	}
	var result Result
	err := r.withTxConn(ctx, func(tx DBTX) error {
		existing, err := r.queries.LockImportPatronRequest(ctx, tx, bundle.PatronRequest.ID)
		exists := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lock patron request %q: %w", bundle.PatronRequest.ID, err)
		}
		if exists {
			switch policy {
			case ConflictPolicyFail:
				return &ConflictError{Resource: "patron request", Identifier: bundle.PatronRequest.ID}
			case ConflictPolicySkip:
				result = Result{Outcome: OutcomeSkipped, Diagnostic: fmt.Sprintf("patron request %q already exists", bundle.PatronRequest.ID)}
				return nil
			case ConflictPolicyUpdate:
				if existing.RequesterReqID != bundle.PatronRequest.RequesterReqID {
					return &ConflictError{Resource: "patron request", Identifier: bundle.PatronRequest.ID, Reason: "requester request ID does not match existing aggregate"}
				}
			default:
				return fmt.Errorf("unsupported conflict policy %q", policy)
			}
		}

		if exists {
			if err := r.queries.UpdateImportedPatronRequest(ctx, tx, importedPatronRequestParams(bundle.PatronRequest)); err != nil {
				return fmt.Errorf("update patron request %q: %w", bundle.PatronRequest.ID, err)
			}
		} else {
			if _, err := r.prQueries.CreatePatronRequest(ctx, tx, bundle.PatronRequest); err != nil {
				return fmt.Errorf("insert patron request %q: %w", bundle.PatronRequest.ID, err)
			}
		}

		itemIDs := make([]string, 0, len(bundle.Items))
		for _, item := range bundle.Items {
			if err := ensureImportParent(ctx, item.ID, bundle.PatronRequest.ID, "item", func(id string) (string, error) {
				return r.queries.GetImportItemParent(ctx, tx, id)
			}); err != nil {
				return err
			}
			item.PrID = bundle.PatronRequest.ID
			if _, err := r.prQueries.SaveItem(ctx, tx, item); err != nil {
				return fmt.Errorf("save item %q: %w", item.ID, err)
			}
			itemIDs = append(itemIDs, item.ID)
		}
		if exists {
			if err := r.queries.DeleteImportedItemsNotPresent(ctx, tx, DeleteImportedItemsNotPresentParams{PrID: bundle.PatronRequest.ID, Ids: itemIDs}); err != nil {
				return fmt.Errorf("synchronize items for patron request %q: %w", bundle.PatronRequest.ID, err)
			}
		}

		notificationIDs := make([]string, 0, len(bundle.Notifications))
		for _, notification := range bundle.Notifications {
			if err := ensureImportParent(ctx, notification.ID, bundle.PatronRequest.ID, "notification", func(id string) (string, error) {
				return r.queries.GetImportNotificationParent(ctx, tx, id)
			}); err != nil {
				return err
			}
			notification.PrID = bundle.PatronRequest.ID
			if _, err := r.prQueries.SaveNotification(ctx, tx, notification); err != nil {
				return fmt.Errorf("save notification %q: %w", notification.ID, err)
			}
			notificationIDs = append(notificationIDs, notification.ID)
		}
		if exists {
			if err := r.queries.DeleteImportedNotificationsNotPresent(ctx, tx, DeleteImportedNotificationsNotPresentParams{PrID: bundle.PatronRequest.ID, Ids: notificationIDs}); err != nil {
				return fmt.Errorf("synchronize notifications for patron request %q: %w", bundle.PatronRequest.ID, err)
			}
		}

		if err := r.saveIllAggregate(ctx, tx, bundle, exists); err != nil {
			return err
		}
		result = Result{Outcome: OutcomeImported}
		return nil
	})
	return result, err
}

func importedPatronRequestParams(params pr_db.CreatePatronRequestParams) UpdateImportedPatronRequestParams {
	return UpdateImportedPatronRequestParams{
		ID: params.ID, CreatedAt: params.CreatedAt, IllRequest: params.IllRequest,
		State: params.State, Side: params.Side, Patron: params.Patron,
		RequesterSymbol: params.RequesterSymbol, SupplierSymbol: params.SupplierSymbol,
		Tenant: params.Tenant, RequesterReqID: params.RequesterReqID,
		NeedsAttention: params.NeedsAttention, LastAction: params.LastAction,
		LastActionOutcome: params.LastActionOutcome, LastActionResult: params.LastActionResult,
		Items: params.Items, Language: params.Language, TerminalState: params.TerminalState,
		UpdatedAt: params.UpdatedAt, IllResponse: params.IllResponse, InternalNote: params.InternalNote,
		NextReqID: params.NextReqID, PrevReqID: params.PrevReqID,
		RetryBibInfo: params.RetryBibInfo, StateModel: params.StateModel,
	}
}

func ensureImportParent(ctx context.Context, id, expectedParent, resource string, getParent func(string) (string, error)) error {
	parent, err := getParent(id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check %s %q ownership: %w", resource, id, err)
	}
	if parent != expectedParent {
		return &ConflictError{Resource: resource, Identifier: id, Reason: fmt.Sprintf("belongs to %q, not %q", parent, expectedParent)}
	}
	return nil
}

func (r *PgImportRepo) saveIllAggregate(ctx common.ExtendedContext, tx DBTX, bundle PatronRequestBundle, updating bool) error {
	requesterRequestID := bundle.PatronRequest.RequesterReqID
	var associated GetImportIllTransactionByRequesterRequestIDRow
	associationExists := false
	if requesterRequestID.Valid {
		var err error
		associated, err = r.queries.GetImportIllTransactionByRequesterRequestID(ctx, tx, requesterRequestID)
		associationExists = err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check ILL association for requester request %q: %w", requesterRequestID.String, err)
		}
	}
	if bundle.IllTransaction == nil {
		if associationExists {
			return &ConflictError{Resource: "ILL transaction", Identifier: associated.ID, Reason: "cannot be omitted from an existing patron request aggregate"}
		}
		if len(bundle.LocatedSuppliers) != 0 {
			return fmt.Errorf("located suppliers require an ILL transaction")
		}
		return nil
	}

	ill := *bundle.IllTransaction
	if ill.RequesterRequestID != requesterRequestID {
		return &ConflictError{Resource: "ILL transaction", Identifier: ill.ID, Reason: "requester request ID does not match patron request"}
	}
	locked, err := r.queries.LockImportIllTransaction(ctx, tx, ill.ID)
	illExists := err == nil
	if err == nil && locked.RequesterRequestID != ill.RequesterRequestID {
		return &ConflictError{Resource: "ILL transaction", Identifier: ill.ID, Reason: "requester request ID does not match existing transaction"}
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock ILL transaction %q: %w", ill.ID, err)
	}
	if !updating && (associationExists || illExists) {
		return &ConflictError{Resource: "ILL transaction", Identifier: ill.ID, Reason: "already belongs to a persisted aggregate"}
	}
	if associationExists && associated.ID != ill.ID {
		return &ConflictError{Resource: "ILL transaction", Identifier: ill.ID, Reason: fmt.Sprintf("requester request ID is already associated with %q", associated.ID)}
	}
	if _, err := r.illQueries.SaveIllTransaction(ctx, tx, ill); err != nil {
		return fmt.Errorf("save ILL transaction %q: %w", ill.ID, err)
	}

	locatedSupplierIDs := make([]string, 0, len(bundle.LocatedSuppliers))
	for _, supplier := range bundle.LocatedSuppliers {
		if err := ensureImportParent(ctx, supplier.ID, ill.ID, "located supplier", func(id string) (string, error) {
			return r.queries.GetImportLocatedSupplierParent(ctx, tx, id)
		}); err != nil {
			return err
		}
		supplier.IllTransactionID = ill.ID
		if _, err := r.illQueries.SaveLocatedSupplier(ctx, tx, supplier); err != nil {
			return fmt.Errorf("save located supplier %q: %w", supplier.ID, err)
		}
		locatedSupplierIDs = append(locatedSupplierIDs, supplier.ID)
	}
	if updating || illExists {
		if err := r.queries.DeleteImportedLocatedSuppliersNotPresent(ctx, tx, DeleteImportedLocatedSuppliersNotPresentParams{IllTransactionID: ill.ID, Ids: locatedSupplierIDs}); err != nil {
			return fmt.Errorf("synchronize located suppliers for ILL transaction %q: %w", ill.ID, err)
		}
	}
	return nil
}

func (r *PgImportRepo) ImportTemplate(ctx common.ExtendedContext, params pr_db.SaveTemplateParams, policy ConflictPolicy) (Result, error) {
	if err := policy.validate(); err != nil {
		return Result{}, err
	}
	var result Result
	err := r.withTxConn(ctx, func(tx DBTX) error {
		matches, err := r.queries.LockImportTemplatesByLabels(ctx, tx, LockImportTemplatesByLabelsParams{Owner: params.Owner, Labels: params.Labels})
		if err != nil {
			return fmt.Errorf("lock templates for owner %q: %w", params.Owner, err)
		}
		if len(matches) != 0 {
			switch policy {
			case ConflictPolicyFail:
				return &ConflictError{Resource: "template", Identifier: params.ID, Reason: "labels overlap an existing template"}
			case ConflictPolicySkip:
				result = Result{Outcome: OutcomeSkipped, Diagnostic: fmt.Sprintf("template labels overlap existing template %q", matches[0].ID)}
				return nil
			case ConflictPolicyUpdate:
				if len(matches) != 1 {
					ids := make([]string, len(matches))
					for i, match := range matches {
						ids[i] = match.ID
					}
					return &ConflictError{Resource: "template", Identifier: params.ID, Reason: "labels overlap multiple templates: " + strings.Join(ids, ", ")}
				}
				params.ID = matches[0].ID
				params.CreatedAt = matches[0].CreatedAt
			default:
				return fmt.Errorf("unsupported conflict policy %q", policy)
			}
		}
		if _, err := r.prQueries.SaveTemplate(ctx, tx, params); err != nil {
			return fmt.Errorf("save template %q: %w", params.ID, err)
		}
		result = Result{Outcome: OutcomeImported}
		return nil
	})
	return result, err
}

func (r *PgImportRepo) ImportBatchAction(ctx common.ExtendedContext, params sched_db.SaveScheduledTaskParams, policy ConflictPolicy) (Result, error) {
	if err := policy.validate(); err != nil {
		return Result{}, err
	}
	var result Result
	err := r.withTxConn(ctx, func(tx DBTX) error {
		existing, err := r.queries.LockImportBatchAction(ctx, tx, LockImportBatchActionParams{Owner: params.Owner, Title: params.Title})
		exists := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lock batch action %q: %w", params.Title.String, err)
		}
		if exists {
			switch policy {
			case ConflictPolicyFail:
				return &ConflictError{Resource: "batch action", Identifier: params.ID, Reason: "owner and title already exist"}
			case ConflictPolicySkip:
				result = Result{Outcome: OutcomeSkipped, Diagnostic: fmt.Sprintf("batch action %q already exists", params.Title.String)}
				return nil
			case ConflictPolicyUpdate:
				params.ID = existing.ID
				params.CreatedAt = existing.CreatedAt
			default:
				return fmt.Errorf("unsupported conflict policy %q", policy)
			}
		}
		if _, err := r.schedQueries.SaveScheduledTask(ctx, tx, params); err != nil {
			return fmt.Errorf("save batch action %q: %w", params.ID, err)
		}
		if _, err := tx.Exec(ctx, "NOTIFY "+sched_db.SchedulerChannel); err != nil {
			return fmt.Errorf("notify scheduler: %w", err)
		}
		result = Result{Outcome: OutcomeImported}
		return nil
	})
	return result, err
}
