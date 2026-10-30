package lms

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
)

// RequestItemIdentifier preserves the distinction needed by automatic scope.
// ItemID is the selected copy; BibliographicID identifies its bibliographic record.
type RequestItemIdentifier struct {
	BibliographicID string
	ItemID          string
}

func (id RequestItemIdentifier) resolve(scope string, normalization *string) (string, string, error) {
	if scope == "auto" {
		scope = "Bibliographic Item"
		if id.ItemID != "" {
			scope = "Item"
		}
	}
	value := id.BibliographicID
	if scope == "Item" && id.ItemID != "" {
		value = id.ItemID
	}
	// Only bibliographic identifiers are normalized, regardless of their format.
	switch scope {
	case "Item":
	case "Title", "Bibliographic Item":
		if id.ItemID != "" {
			return "", "", fmt.Errorf("selected item requires NCIP Item scope, configured scope is %q", scope)
		}
		value = normalizeBibIdentifier(value, normalization)
	default:
		return "", "", fmt.Errorf("unsupported RequestItem scope %q", scope)
	}
	if strings.TrimSpace(value) == "" {
		return "", "", fmt.Errorf("missing identifier for RequestItem scope %q", scope)
	}
	return value, scope, nil
}

// ValidateSelectedItemScope rejects a known policy-selected item when the
// supplier's enabled NCIP RequestItem integration expects a bibliographic ID.
// Explicit bibliographic scopes must not silently broaden a policy-selected copy.
func ValidateSelectedItemScope(entry dirapi.Entry) error {
	effective, err := profiles.Resolve(entry)
	if err != nil {
		return err
	}
	cfg := effective.LMS
	if cfg == nil || cfg.Address == "" {
		return nil
	}
	return validateSelectedItemScope(*cfg)
}

func validateSelectedItemScope(cfg dirapi.LmsConfig) error {
	if cfg.RequestItemEnabled != nil && !*cfg.RequestItemEnabled {
		return nil
	}
	scope := "auto"
	if cfg.RequestItemRequestScopeType != nil {
		scope = *cfg.RequestItemRequestScopeType
	}
	if scope != "Item" && scope != "auto" {
		return fmt.Errorf("policy-selected item requires NCIP Item scope, configured scope is %q", scope)
	}
	return nil
}

// ValidateSelectedItemScopeForAdapter checks the resolved configuration of the
// adapter that will perform local fulfillment. Manual and non-NCIP adapters do
// not interpret identifiers using NCIP request scope.
func ValidateSelectedItemScopeForAdapter(adapter LmsAdapter) error {
	switch a := adapter.(type) {
	case *LmsAdapterNcip:
		return validateSelectedItemScope(a.config)
	case *LmsAdapterNcip1:
		return validateSelectedItemScope(a.config)
	default:
		return nil
	}
}
