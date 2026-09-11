// Package profiles resolves host integrations without modifying directory records.
package profiles

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
)

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
	// These are the existing protocol defaults; no installation-specific values.
	merge(l, object{"ncipNamespaceEnabled": true, "bibIdNormalization": "none", "requestItemRequestType": "Page", "requestItemRequestScopeType": "Item", "requestItemBibIdCode": "SYSNUMBER", "requestItemPickupLocationEnabled": true, "lookupUserEnabled": true, "acceptItemEnabled": true, "checkInItemEnabled": true, "checkOutItemEnabled": true, "requestItemEnabled": true}, "lmsConfig", "Generic", e.Origins)
	if vendor == "Sierra" {
		merge(l, object{"requestItemRequestType": "Hold", "requestItemRequestScopeType": "Title", "bibIdNormalization": "sierra"}, "lmsConfig", vendor, e.Origins)
	}
	if vendor == "Koha" {
		merge(l, object{"ncipNamespaceEnabled": false}, "lmsConfig", vendor, e.Origins)
	}
	merge(l, rawL, "lmsConfig", "directory", e.Origins)
	if profile != "Generic" {
		h := object{"opac": object{"availabilityRule": "availableNow", "requireLocalLocation": true, "includeItemId": true, "includeItemLoanPolicy": true, "allCirculations": true}}
		if profile == "Sierra" {
			h = object{"opac": object{"availabilityRule": "publicNote", "availablePublicNotes": []any{"AVAILABLE", "CHECK SHELVES", "CHECK SHELF"}, "shelvingLocationSource": "localLocation", "includeItemId": false, "includeItemLoanPolicy": false}}
		}
		if profile == "FOLIO" {
			h["opac"].(object)["includeTemporaryLocation"] = true
		}
		if profile == "Koha" {
			h = object{"marc": object{"mainField": "952", "locationSubField": "b", "shelvingLocationSubField": "c", "callNumberSubField": "o", "availability": []any{object{"subField": "7", "operator": "equals", "value": "0"}, object{"subField": "q", "operator": "absent"}}}}
		}
		merge(c, object{"holdingsFormat": h}, "catalogConfig", profile, e.Origins)
	}
	// Switching parser replaces the entire profile parser, including its rules.
	if h, ok := rawC["holdingsFormat"].(object); ok && len(h) > 0 {
		if len(h) != 1 {
			return nil, fmt.Errorf("catalog profile %s: holdingsFormat must select exactly one parser", profile)
		}
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
	if profile != "Generic" {
		syntax := "opac"
		if h, ok := c["holdingsFormat"].(object); ok {
			if _, ok := h["opac"]; !ok {
				syntax = "xml"
			}
		}
		if z, ok := c["zoom"].(object); ok {
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
			if _, ok := s["recordSchema"]; !ok {
				schema := "opac"
				if syntax == "xml" {
					schema = "marcxml"
				}
				s["recordSchema"] = schema
				e.Origins["catalogConfig.sru.recordSchema"] = profile
			}
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
	// Only behavior settings are logged: endpoints, options, credentials, agencies,
	// patron details, and local policies are deliberately excluded.
	slog.Debug("resolved host profiles", "configuration", e.Diagnostics())
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
