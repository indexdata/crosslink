package importdb

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	ill_db "github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/repo"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
)

type ConflictPolicy string

const (
	ConflictPolicyFail   ConflictPolicy = "fail"
	ConflictPolicySkip   ConflictPolicy = "skip"
	ConflictPolicyUpdate ConflictPolicy = "update"
)

func ParseConflictPolicy(value string) (ConflictPolicy, error) {
	switch ConflictPolicy(value) {
	case "", ConflictPolicyFail:
		return ConflictPolicyFail, nil
	case ConflictPolicySkip:
		return ConflictPolicySkip, nil
	case ConflictPolicyUpdate:
		return ConflictPolicyUpdate, nil
	default:
		return "", fmt.Errorf("unknown conflict policy: %s", value)
	}
}

func (p ConflictPolicy) validate() error {
	switch p {
	case ConflictPolicyFail, ConflictPolicySkip, ConflictPolicyUpdate:
		return nil
	default:
		return fmt.Errorf("unsupported conflict policy %q", p)
	}
}

type Outcome string

const (
	OutcomeImported Outcome = "imported"
	OutcomeSkipped  Outcome = "skipped"
)

type Result struct {
	Outcome    Outcome
	Diagnostic string
}

type PatronRequestBundle struct {
	PatronRequest    pr_db.CreatePatronRequestParams
	Items            []pr_db.SaveItemParams
	Notifications    []pr_db.SaveNotificationParams
	IllTransaction   *ill_db.SaveIllTransactionParams
	LocatedSuppliers []ill_db.SaveLocatedSupplierParams
}

type ImportRepo interface {
	repo.Transactional[ImportRepo]
	ImportPatronRequest(common.ExtendedContext, PatronRequestBundle, ConflictPolicy) (Result, error)
	ImportTemplate(common.ExtendedContext, pr_db.SaveTemplateParams, ConflictPolicy) (Result, error)
	ImportBatchAction(common.ExtendedContext, sched_db.SaveScheduledTaskParams, ConflictPolicy) (Result, error)
}

type ConflictError struct {
	Resource   string
	Identifier string
	Reason     string
}

func (e *ConflictError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s %q already exists", e.Resource, e.Identifier)
	}
	return fmt.Sprintf("%s %q conflicts: %s", e.Resource, e.Identifier, e.Reason)
}
