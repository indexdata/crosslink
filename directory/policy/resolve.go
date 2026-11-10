package policy

import (
	"encoding/json"
	"fmt"
)

// redactedValue replaces protected values the caller may not see, matching
// the API's existing sanitization of protected strings.
var redactedValue = json.RawMessage(`""`)

// Level is one entry's stored configuration within an inheritance chain.
type Level struct {
	// Type is the entry type.
	Type string
	// Tenant is the owning tenant; nil for an unowned entry.
	Tenant *string
	// Configs maps a container name to its stored JSON object. A missing
	// container means the entry stores no value for it.
	Configs map[string]json.RawMessage
	// Redact hides this entry's protected values: they are resolved as ""
	// so that redaction never changes which entry supplies a merge unit.
	Redact bool
}

// Resolve merges the annotated containers of chain[0] with the values its
// ancestors may pass down. chain lists the entry first, then its parent and
// grandparent; a chain may stop early when a parent is missing.
//
// Containers without a policy are returned exactly as chain[0] stores them.
// For each field or merge unit, the nearest entry that stores any of its
// fields supplies all of them. A winning off value is omitted. Protected
// values are never taken from an entry with a different tenant.
func (p *Policy) Resolve(chain []Level) (map[string]json.RawMessage, error) {
	if len(chain) == 0 {
		return nil, fmt.Errorf("resolve inheritance: empty chain")
	}
	result := map[string]json.RawMessage{}
	for name, value := range chain[0].Configs {
		if _, annotated := p.containers[name]; !annotated {
			result[name] = value
		}
	}
	for _, name := range p.names {
		value, err := p.containers[name].resolve(chain)
		if err != nil {
			return nil, err
		}
		if value != nil {
			result[name] = value
		}
	}
	return result, nil
}

func (c *Container) resolve(chain []Level) (json.RawMessage, error) {
	if c.Atomic {
		for i, level := range chain {
			if i > 0 && !c.Eligible(level.Type) {
				break
			}
			if value, ok := level.Configs[c.Name]; ok && !isNull(value) {
				return value, nil
			}
		}
		return nil, nil
	}

	levels := make([]map[string]json.RawMessage, len(chain))
	for i, level := range chain {
		value, ok := level.Configs[c.Name]
		if !ok || isNull(value) {
			continue
		}
		if err := json.Unmarshal(value, &levels[i]); err != nil {
			return nil, fmt.Errorf("resolve inheritance: decode %s: %w", c.Name, err)
		}
		if levels[i] == nil {
			levels[i] = map[string]json.RawMessage{}
		}
	}

	merged := map[string]json.RawMessage{}
	done := map[string]bool{}
	for _, children := range levels {
		for child := range children {
			field := c.Child(child)
			item := child
			if field.Unit != "" {
				item = "unit:" + field.Unit
			}
			if done[item] {
				continue
			}
			done[item] = true
			members := []string{child}
			if field.Unit != "" {
				members = c.unitMembers(field.Unit)
			}
			c.resolveItem(chain, levels, field, members, merged)
		}
	}

	for child, value := range merged {
		if offValue := c.Child(child).OffValue; offValue != nil && jsonEqual(value, offValue) {
			delete(merged, child)
		}
	}
	if len(merged) == 0 && levels[0] == nil {
		return nil, nil
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("resolve inheritance: encode %s: %w", c.Name, err)
	}
	return data, nil
}

// resolveItem copies the members of one field or merge unit from the nearest
// eligible entry that stores any of them.
func (c *Container) resolveItem(chain []Level, levels []map[string]json.RawMessage, field Field, members []string, merged map[string]json.RawMessage) {
	for i, children := range levels {
		if i > 0 && (field.Root == RootNone || !field.Eligible(chain[i].Type)) {
			return
		}
		if !storesAny(children, members) {
			continue
		}
		for _, member := range members {
			value, ok := children[member]
			if !ok {
				continue
			}
			if c.Child(member).Protected {
				if i > 0 && !sameTenant(chain[0].Tenant, chain[i].Tenant) {
					continue
				}
				if chain[i].Redact {
					value = redactedValue
				}
			}
			merged[member] = value
		}
		return
	}
}

func (c *Container) unitMembers(unit string) []string {
	var members []string
	for name, field := range c.Children {
		if field.Unit == unit {
			members = append(members, name)
		}
	}
	return members
}

func storesAny(children map[string]json.RawMessage, members []string) bool {
	for _, member := range members {
		if _, ok := children[member]; ok {
			return true
		}
	}
	return false
}

// sameTenant treats two unowned entries as belonging to the same tenant: no
// tenant boundary is crossed between entries that only consortial
// administrators manage.
func sameTenant(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
