package importdb

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/indexdata/crosslink/directory/policy"
)

// checkConfigPolicy rejects configuration values that the record's entry type
// may not store. Import records are complete and replace the entry's local
// configuration, so null is always allowed: it clears the stored value.
func (r *PgImportRepo) checkConfigPolicy(data model.EntryData) error {
	if r.inheritance == nil {
		return nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode entry configuration: %w", err)
	}
	var supplied map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &supplied); err != nil {
		return fmt.Errorf("decode entry configuration: %w", err)
	}
	violations, err := r.inheritance.CheckWrite(data.Type, supplied, nil)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return errors.New(policy.ViolationMessage(violations))
	}
	return nil
}
