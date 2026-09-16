// Package profiles resolves host integrations without modifying directory records.
package profiles

import (
	"encoding/json"
	"fmt"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
)

// Effective holds resolved LMS and catalog configurations and their setting origins.
// Values returned by Resolve are validated and independent of the directory entry.
// LMS and Catalog remain nil when the corresponding entry configuration is absent;
// selecting a profile alone does not enable a catalog lookup endpoint.
type Effective struct {
	LMS            *dirapi.LmsConfig
	Catalog        *dirapi.CatalogConfig
	LMSVendor      string
	CatalogProfile string
	// Origins maps configuration paths to Generic, a profile name, or directory.
	Origins map[string]string
}

type object = map[string]any

func asObject(v any) object {
	b, _ := json.Marshal(v)
	var m object
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = object{}
	}
	return m
}

func merge(dst, src object, path, origin string, origins map[string]string) {
	for k, v := range src {
		if v == nil {
			continue
		}
		key := path + "." + k
		if sub, ok := v.(map[string]any); ok {
			old, ok := dst[k].(map[string]any)
			if !ok {
				old = object{}
				dst[k] = old
			}
			merge(old, sub, key, origin, origins)
		} else {
			dst[k] = v
			origins[key] = origin
		}
	}
}

func selected(m object, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}
func supported(name, setting string) error {
	switch name {
	case "Generic", "Alma", "Sierra", "Koha", "FOLIO":
		return nil
	case "WMS", "Aleph":
		return fmt.Errorf("%s profile %s is not yet supported", setting, name)
	default:
		return fmt.Errorf("%s: unknown profile %q", setting, name)
	}
}

// Resolve applies profile defaults and explicit directory overrides to produce
// validated effective LMS and catalog configurations without mutating entry.
func Resolve(entry dirapi.Entry) (*Effective, error) {
	rawL, rawC := asObject(entry.LmsConfig), asObject(entry.CatalogConfig)
	vendor := selected(rawL, "vendor", "Generic")
	profile := selected(rawC, "profile", vendor)
	if err := supported(vendor, "lmsConfig.vendor"); err != nil {
		return nil, err
	}
	if err := supported(profile, "catalogConfig.profile"); err != nil {
		return nil, err
	}
	e := &Effective{LMSVendor: vendor, CatalogProfile: profile, Origins: map[string]string{}}
	l, c := object{}, object{}
	merge(l, builtins["Generic"].LMS, "lmsConfig", "Generic", e.Origins)
	if vendor != "Generic" {
		merge(l, builtins[vendor].LMS, "lmsConfig", vendor, e.Origins)
	}
	merge(l, rawL, "lmsConfig", "directory", e.Origins)
	merge(c, builtins[profile].Catalog, "catalogConfig", profile, e.Origins)
	// Switching parser replaces the entire profile parser, including its rules.
	if h, ok := rawC["holdingsFormat"].(object); ok && len(h) > 0 {
		for parser := range h {
			if defaults, ok := c["holdingsFormat"].(object); ok {
				if _, same := defaults[parser]; !same {
					delete(c, "holdingsFormat")
					for key := range e.Origins {
						if strings.HasPrefix(key, "catalogConfig.holdingsFormat.") {
							delete(e.Origins, key)
						}
					}
				}
			}
		}
	}
	// An empty legacy holdings object means the existing generic default.
	if h, ok := rawC["holdingsFormat"].(object); ok && len(h) == 0 && profile != "Generic" {
		delete(rawC, "holdingsFormat")
	}
	merge(c, rawC, "catalogConfig", "directory", e.Origins)
	if z, ok := c["zoom"].(object); ok {
		// MARC and reservoir use the adapter's legacy MARC wire syntax;
		// ZOOM converts received records to XML before passing them to parsers.
		syntax := "usmarc"
		if h, ok := c["holdingsFormat"].(object); ok {
			if _, ok := h["opac"]; ok {
				syntax = "opac"
			} else if _, ok := h["marc21plus1"]; ok {
				syntax = "xml"
			}
		}
		options, ok := z["options"].(object)
		if !ok {
			options = object{}
			z["options"] = options
		}
		if _, ok := options["preferredRecordSyntax"]; !ok {
			options["preferredRecordSyntax"] = syntax
			e.Origins["catalogConfig.zoom.options.preferredRecordSyntax"] = profile
		}
	}
	if s, ok := c["sru"].(object); ok {
		// SRU delivers MARC-based formats as marcxml and OPAC as opac.
		schema := "marcxml"
		if h, ok := c["holdingsFormat"].(object); ok {
			if _, ok := h["opac"]; ok {
				schema = "opac"
			}
		}
		if _, ok := s["recordSchema"]; !ok {
			s["recordSchema"] = schema
			e.Origins["catalogConfig.sru.recordSchema"] = profile
		}
	}
	materializeCatalogDefaults(c, e.Origins)

	if entry.LmsConfig != nil {
		b, _ := json.Marshal(l)
		if err := json.Unmarshal(b, &e.LMS); err != nil {
			return nil, err
		}
	}
	if entry.CatalogConfig != nil {
		b, _ := json.Marshal(c)
		if err := json.Unmarshal(b, &e.Catalog); err != nil {
			return nil, err
		}
	}
	if err := e.validate(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Effective) validate() error {
	if l := e.LMS; l != nil {
		if (l.Address == "") != (l.FromAgency == "") {
			return fmt.Errorf("LMS profile %s: lmsConfig.address and fromAgency must both be configured", e.LMSVendor)
		}
		if l.BibIdNormalization != nil && *l.BibIdNormalization != "none" && *l.BibIdNormalization != "sierra" {
			return fmt.Errorf("LMS profile %s: unsupported bibIdNormalization", e.LMSVendor)
		}
	}
	if c := e.Catalog; c != nil {
		bad := func(setting string) error {
			return fmt.Errorf("catalog profile %s: invalid %s", e.CatalogProfile, setting)
		}
		if c.Sru != nil && c.Sru.Address == "" {
			return bad("sru.address")
		}
		if c.Zoom != nil && c.Zoom.Address == "" {
			return bad("zoom.address")
		}
		if c.Sru != nil && c.Zoom != nil {
			return bad("simultaneous sru and zoom endpoints")
		}
		if c.MetadataFormat != nil && c.MetadataFormat.Marc21 == nil {
			return bad("metadataFormat.marc21")
		}
		if c.QueryConfig != nil && c.QueryConfig.Type != nil && *c.QueryConfig.Type != dirapi.Pqf && *c.QueryConfig.Type != dirapi.Cql {
			return bad("queryConfig.type")
		}
		if h := c.HoldingsFormat; h != nil {
			n := 0
			if h.Marc != nil {
				n++
			}
			if h.Opac != nil {
				n++
			}
			if h.Reservoir != nil {
				n++
			}
			if h.Marc21plus1 != nil {
				n++
			}
			if n != 1 {
				return bad("holdingsFormat must set marc, opac, reservoir, or marc21plus1 (exactly one parser)")
			}
			if m := h.Marc; m != nil && m.Availability != nil {
				for _, r := range *m.Availability {
					if r.SubField == "" || (r.Operator != "equals" && r.Operator != "absent") || (r.Operator == "equals" && r.Value == nil) {
						return bad("holdingsFormat.marc.availability")
					}
				}
			}
			if o := h.Opac; o != nil {
				if o.AvailabilityRule != nil && *o.AvailabilityRule != "availableNow" && *o.AvailabilityRule != "publicNote" {
					return bad("holdingsFormat.opac.availabilityRule")
				}
				if o.ShelvingLocationSource != nil && *o.ShelvingLocationSource != "localLocation" && *o.ShelvingLocationSource != "shelvingLocation" {
					return bad("holdingsFormat.opac.shelvingLocationSource")
				}
				if o.AvailabilityRule != nil && *o.AvailabilityRule == "publicNote" && o.AvailablePublicNotes == nil {
					return bad("holdingsFormat.opac.availablePublicNotes is required")
				}
			}
		}
	}
	return nil
}
