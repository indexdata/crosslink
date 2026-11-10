// Package policy interprets the inheritance and editability annotations that
// directory/api.yaml declares on entry configuration fields.
//
// Annotations are placed on EntryProperties' configuration containers
// (illConfig, lmsConfig, catalogConfig, holdingsPolicy) and on their direct
// children:
//
//   - x-inheritance-root: the highest ancestor type consulted for a value
//     (Consortium or Institution), or none when the field never inherits.
//   - x-editable-on: the entry types that may store a local value.
//   - x-merge-unit: fields sharing a unit name are always taken from the same
//     entry. On a container, the whole container is one unit.
//   - x-off-value: a stored value meaning "disabled"; it blocks inheritance
//     and is omitted from resolved views.
//
// A child without an annotation takes the container's setting. A child's
// x-editable-on list replaces the container's list.
package policy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
)

// Entry types, as declared by the EntryType schema.
const (
	TypeConsortium  = "Consortium"
	TypeInstitution = "Institution"
	TypeBranch      = "Branch"
)

// RootNone marks a field that never inherits.
const RootNone = "none"

const (
	extRoot     = "x-inheritance-root"
	extEditable = "x-editable-on"
	extUnit     = "x-merge-unit"
	extOffValue = "x-off-value"
	extTags     = "x-oapi-codegen-extra-tags"
)

var policyExtensions = []string{extRoot, extEditable, extUnit, extOffValue}

// rank orders entry types from the bottom of the hierarchy to the top.
var rank = map[string]int{TypeBranch: 0, TypeInstitution: 1, TypeConsortium: 2}

// Field is the effective policy of one configuration field.
type Field struct {
	// Root is TypeConsortium, TypeInstitution or RootNone.
	Root string
	// EditableOn lists the entry types that may store a value; nil means any type.
	EditableOn []string
	// Unit names the merge unit; empty when the field resolves on its own.
	Unit string
	// OffValue is the JSON encoding of the field's off value, or nil.
	OffValue json.RawMessage
	// Protected fields are redacted for callers who may not see sensitive data.
	Protected bool
}

// Editable reports whether an entry of entryType may store a local value.
func (f Field) Editable(entryType string) bool {
	return f.EditableOn == nil || slices.Contains(f.EditableOn, entryType)
}

// Eligible reports whether a value stored on an ancestor of ancestorType may
// be inherited.
func (f Field) Eligible(ancestorType string) bool {
	rootRank, ok := rank[f.Root]
	if !ok {
		return false
	}
	ancestorRank, ok := rank[ancestorType]
	return ok && ancestorRank <= rootRank
}

// Container is the policy of one entry configuration property.
type Container struct {
	Name string
	// Field holds the container-level settings that children inherit.
	Field
	// Atomic containers resolve and validate as a single value.
	Atomic bool
	// Children holds the effective policy of every declared child property.
	Children map[string]Field
	// schemas holds the canonical child schemas, which variants may share.
	schemas map[string]*openapi3.Schema
}

// Child returns the effective policy of a child. Undeclared children get the
// container's settings.
func (c *Container) Child(name string) Field {
	if field, ok := c.Children[name]; ok {
		return field
	}
	return Field{Root: c.Root, EditableOn: c.EditableOn}
}

// Policy is the compiled set of annotated containers.
type Policy struct {
	containers map[string]*Container
	names      []string
}

// Containers returns the annotated container names in sorted order.
func (p *Policy) Containers() []string {
	return p.names
}

// Container returns the policy of an annotated container.
func (p *Policy) Container(name string) (*Container, bool) {
	c, ok := p.containers[name]
	return c, ok
}

// Variant schemas that write the same containers. Their properties must all
// have a canonical policy, and they must not declare policy themselves.
var variantSchemas = []string{"EntryPatch", "ImportEntryDataProperties"}

// Compile builds the policy from the EntryProperties schema and checks that
// create, PATCH and import variants are consistent with it.
func Compile(spec *openapi3.T) (*Policy, error) {
	if spec == nil || spec.Components == nil {
		return nil, fmt.Errorf("compile inheritance policy: specification has no components")
	}
	canonical := spec.Components.Schemas["EntryProperties"]
	if canonical == nil || canonical.Value == nil {
		return nil, fmt.Errorf("compile inheritance policy: EntryProperties schema not found")
	}
	p := &Policy{containers: map[string]*Container{}}
	for name, property := range properties(canonical) {
		if !hasPolicy(property.Value) {
			if err := rejectNestedPolicy(property, "EntryProperties."+name); err != nil {
				return nil, err
			}
			continue
		}
		container, err := compileContainer(name, property)
		if err != nil {
			return nil, err
		}
		p.containers[name] = container
		p.names = append(p.names, name)
	}
	sort.Strings(p.names)
	for _, variant := range variantSchemas {
		if err := p.checkVariant(spec, variant); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func compileContainer(name string, property *openapi3.SchemaRef) (*Container, error) {
	base, err := readField(name, property.Value)
	if err != nil {
		return nil, err
	}
	if base.Root == "" {
		return nil, fmt.Errorf("compile inheritance policy: %s declares policy without %s", name, extRoot)
	}
	if len(base.OffValue) > 0 {
		return nil, fmt.Errorf("compile inheritance policy: %s: %s is only supported on fields", name, extOffValue)
	}
	container := &Container{Name: name, Field: base, Atomic: base.Unit != "", Children: map[string]Field{}, schemas: map[string]*openapi3.Schema{}}
	container.Unit = ""
	units := map[string]Field{}
	for childName, child := range properties(property) {
		path := name + "." + childName
		if err := rejectNestedPolicy(child, path); err != nil {
			return nil, err
		}
		field, err := readField(path, child.Value)
		if err != nil {
			return nil, err
		}
		if container.Atomic && hasPolicy(child.Value) {
			return nil, fmt.Errorf("compile inheritance policy: %s: atomic container %s cannot declare child policy", path, name)
		}
		if field.Root == "" {
			field.Root = base.Root
		}
		if field.EditableOn == nil {
			field.EditableOn = base.EditableOn
		}
		if field.Unit != "" {
			if first, ok := units[field.Unit]; ok && (first.Root != field.Root || !slices.Equal(first.EditableOn, field.EditableOn)) {
				return nil, fmt.Errorf("compile inheritance policy: %s: merge unit %s mixes inheritance or editability settings", path, field.Unit)
			}
			units[field.Unit] = field
		}
		container.Children[childName] = field
		container.schemas[childName] = child.Value
	}
	return container, nil
}

func readField(path string, schema *openapi3.Schema) (Field, error) {
	var field Field
	if schema == nil {
		return field, nil
	}
	if raw, ok := schema.Extensions[extRoot]; ok {
		if err := decodeExtension(raw, &field.Root); err != nil {
			return field, fmt.Errorf("compile inheritance policy: %s: %s: %w", path, extRoot, err)
		}
		if _, known := rank[field.Root]; !known && field.Root != RootNone || field.Root == TypeBranch {
			return field, fmt.Errorf("compile inheritance policy: %s: unknown %s %q", path, extRoot, field.Root)
		}
	}
	if raw, ok := schema.Extensions[extEditable]; ok {
		if err := decodeExtension(raw, &field.EditableOn); err != nil {
			return field, fmt.Errorf("compile inheritance policy: %s: %s: %w", path, extEditable, err)
		}
		if field.EditableOn == nil {
			field.EditableOn = []string{}
		}
		for _, entryType := range field.EditableOn {
			if _, known := rank[entryType]; !known {
				return field, fmt.Errorf("compile inheritance policy: %s: unknown entry type %q in %s", path, entryType, extEditable)
			}
		}
	}
	if raw, ok := schema.Extensions[extUnit]; ok {
		if err := decodeExtension(raw, &field.Unit); err != nil || field.Unit == "" {
			return field, fmt.Errorf("compile inheritance policy: %s: %s must be a non-empty string", path, extUnit)
		}
	}
	if raw, ok := schema.Extensions[extOffValue]; ok {
		value, err := json.Marshal(raw)
		if err != nil {
			return field, fmt.Errorf("compile inheritance policy: %s: %s: %w", path, extOffValue, err)
		}
		field.OffValue = value
	}
	if raw, ok := schema.Extensions[extTags]; ok {
		var tags map[string]string
		if err := decodeExtension(raw, &tags); err != nil {
			return field, fmt.Errorf("compile inheritance policy: %s: %s: %w", path, extTags, err)
		}
		field.Protected = tags["protected"] == "true"
	}
	return field, nil
}

// decodeExtension converts an extension value, whatever form the loader
// produced, into target.
func decodeExtension(raw any, target any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func hasPolicy(schema *openapi3.Schema) bool {
	if schema == nil {
		return false
	}
	for _, name := range policyExtensions {
		if _, ok := schema.Extensions[name]; ok {
			return true
		}
	}
	return false
}

// rejectNestedPolicy fails when policy annotations appear below the supported
// depth, where they would otherwise be silently ignored.
func rejectNestedPolicy(ref *openapi3.SchemaRef, path string) error {
	for name, property := range properties(ref) {
		if hasPolicy(property.Value) {
			return fmt.Errorf("compile inheritance policy: %s.%s: policy is supported only on entry configuration containers and their direct children", path, name)
		}
	}
	return nil
}

func (p *Policy) checkVariant(spec *openapi3.T, variant string) error {
	ref := spec.Components.Schemas[variant]
	if ref == nil || ref.Value == nil {
		return fmt.Errorf("compile inheritance policy: %s schema not found", variant)
	}
	for name, property := range properties(ref) {
		path := variant + "." + name
		if hasPolicy(property.Value) {
			return fmt.Errorf("compile inheritance policy: %s: declare policy on EntryProperties only", path)
		}
		container, ok := p.containers[name]
		if !ok {
			continue
		}
		for childName, child := range properties(property) {
			// A variant may reuse the canonical schema, annotations included.
			if hasPolicy(child.Value) && child.Value != container.schemas[childName] {
				return fmt.Errorf("compile inheritance policy: %s.%s: declare policy on EntryProperties only", path, childName)
			}
			if _, known := container.Children[childName]; !known && !container.Atomic {
				return fmt.Errorf("compile inheritance policy: %s.%s has no canonical policy in EntryProperties.%s", path, childName, name)
			}
		}
	}
	return nil
}

// properties returns the properties of a schema, flattening allOf and the
// object alternative of anyOf/oneOf so that nullable wrappers and references
// expose the underlying object.
func properties(ref *openapi3.SchemaRef) map[string]*openapi3.SchemaRef {
	result := map[string]*openapi3.SchemaRef{}
	collectProperties(ref, result, map[*openapi3.Schema]bool{})
	return result
}

func collectProperties(ref *openapi3.SchemaRef, result map[string]*openapi3.SchemaRef, seen map[*openapi3.Schema]bool) {
	if ref == nil || ref.Value == nil || seen[ref.Value] {
		return
	}
	schema := ref.Value
	seen[schema] = true
	for name, property := range schema.Properties {
		if _, exists := result[name]; !exists {
			result[name] = property
		}
	}
	for _, group := range [][]*openapi3.SchemaRef{schema.AllOf, schema.AnyOf, schema.OneOf} {
		for _, member := range group {
			collectProperties(member, result, seen)
		}
	}
}

// jsonEqual reports whether two JSON documents encode the same value.
func jsonEqual(a, b json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func isNull(value json.RawMessage) bool {
	var decoded any
	return len(value) == 0 || json.Unmarshal(value, &decoded) == nil && decoded == nil
}
