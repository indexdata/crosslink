package lms

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/ncip"
)

// RequestItemIdentifier preserves the distinction needed by automatic scope.
// ItemID is the selected copy; BibliographicID identifies its bibliographic record.
type RequestItemIdentifier struct {
	BibliographicID string
	ItemID          string
}

const autoRequestScope = "auto"

func (id RequestItemIdentifier) resolve(config dirapi.LmsConfig) (string, string, error) {
	scope := autoRequestScope
	if config.RequestItemRequestScopeType != nil {
		scope = *config.RequestItemRequestScopeType
	}
	bibScope := string(ncip.BibliographicItem)
	if config.BibItemScopeName != nil {
		bibScope = *config.BibItemScopeName
	}
	if scope == autoRequestScope {
		scope = bibScope
		if id.ItemID != "" {
			scope = string(ncip.Item)
		}
	}
	value := id.BibliographicID
	if scope == string(ncip.Item) {
		if id.ItemID != "" {
			value = id.ItemID
		}
	} else {
		if scope != bibScope && scope != string(ncip.BibliographicItem) && scope != "Title" {
			return "", "", fmt.Errorf("unsupported RequestItem scope %q", scope)
		}
		if id.ItemID != "" {
			return "", "", fmt.Errorf("selected item requires NCIP Item scope, configured scope is %q", scope)
		}
		// Only bibliographic identifiers are normalized, regardless of their format.
		value = normalizeBibIdentifier(value, config.BibIdNormalization)
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
	scope := autoRequestScope
	if cfg.RequestItemRequestScopeType != nil {
		scope = *cfg.RequestItemRequestScopeType
	}
	if scope != string(ncip.Item) && scope != autoRequestScope {
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
