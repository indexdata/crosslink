package policy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Violation is a supplied value that the entry type may not store.
type Violation struct {
	// Path is the dotted field path, such as lmsConfig.address.
	Path      string
	EntryType string
}

func (v Violation) Error() string {
	return fmt.Sprintf("%s is not editable on %s", v.Path, v.EntryType)
}

// CheckWrite validates the configuration containers in supplied, a JSON
// object of the request's top-level properties, against entryType.
//
// Null values are always allowed: they remove a local value, even where the
// entry type may not set one. A supplied value equal to the corresponding
// value in stored (the entry's current local configuration, keyed by
// container) is allowed as a no-op. Violations are returned in path order.
func (p *Policy) CheckWrite(entryType string, supplied map[string]json.RawMessage, stored map[string]json.RawMessage) ([]Violation, error) {
	var violations []Violation
	for _, name := range p.names {
		value, ok := supplied[name]
		if !ok || isNull(value) {
			continue
		}
		container := p.containers[name]
		if container.Atomic {
			if !container.Editable(entryType) && !jsonEqual(value, stored[name]) {
				violations = append(violations, Violation{Path: name, EntryType: entryType})
			}
			continue
		}
		var children map[string]json.RawMessage
		if err := json.Unmarshal(value, &children); err != nil {
			return nil, fmt.Errorf("check %s: %w", name, err)
		}
		var storedChildren map[string]json.RawMessage
		if current, ok := stored[name]; ok && !isNull(current) {
			if err := json.Unmarshal(current, &storedChildren); err != nil {
				return nil, fmt.Errorf("check stored %s: %w", name, err)
			}
		}
		for child, childValue := range children {
			if isNull(childValue) || container.Child(child).Editable(entryType) {
				continue
			}
			if current, ok := storedChildren[child]; ok && jsonEqual(childValue, current) {
				continue
			}
			violations = append(violations, Violation{Path: name + "." + child, EntryType: entryType})
		}
	}
	sort.Slice(violations, func(i, j int) bool { return violations[i].Path < violations[j].Path })
	return violations, nil
}

// ViolationMessage joins violations into one client-facing message.
func ViolationMessage(violations []Violation) string {
	messages := make([]string, len(violations))
	for i, violation := range violations {
		messages[i] = violation.Error()
	}
	return strings.Join(messages, "; ")
}
