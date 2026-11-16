package lms

import (
	"fmt"
	"strings"

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
		// Only bibliographic identifiers are normalized, regardless of their format.
		value = normalizeBibIdentifier(value, config.BibIdNormalization)
	}
	if strings.TrimSpace(value) == "" {
		return "", "", fmt.Errorf("missing identifier for RequestItem scope %q", scope)
	}
	return value, scope, nil
}
