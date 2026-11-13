package sched_service

import (
	"errors"
	"fmt"
	"time"

	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
)

// ValidateArchiveConfig validates the common configuration of
// patron-request and ILL-transaction archive actions.
func ValidateArchiveConfig(actionName, owner, selector string, params map[string]any) error {
	if actionName != string(schedoapi.ArchiveBorrowingRequests) && actionName != string(schedoapi.ArchiveLendingRequests) && actionName != string(schedoapi.ArchiveIllTransactions) {
		return nil
	}
	if owner == "" {
		return fmt.Errorf("%s requires an owner symbol", actionName)
	}
	var err error
	if actionName == string(schedoapi.ArchiveIllTransactions) {
		_, err = ill_db.ParseIllTransactionsCql(selector, 2)
	} else {
		_, err = pr_db.ParsePatronRequestsCql(selector)
	}
	if err != nil {
		return fmt.Errorf("invalid %s batchQuery: %w", actionName, err)
	}
	intervalText, ok := params["interval"].(string)
	if !ok {
		return fmt.Errorf("interval is required for %s", actionName)
	}
	interval, err := time.ParseDuration(intervalText)
	if err != nil || interval <= 0 {
		return errors.New("interval must be a positive Go duration")
	}
	if value, exists := params["anonymize"]; exists {
		if _, ok := value.(bool); !ok {
			return errors.New("anonymize must be a boolean")
		}
	}
	return nil
}
