package lms

import "strings"

// normalizeBibIdentifier applies configured circulation normalization independently
// of catalog query normalization. Identifiers without digits remain unchanged.
func normalizeBibIdentifier(id string, normalization *string) string {
	if normalization == nil {
		return id
	}
	switch *normalization {
	case "sierra":
		id = strings.TrimPrefix(id, ".b")
		if len(id) > 1 && id[len(id)-1] >= '0' && id[len(id)-1] <= '9' {
			id = id[:len(id)-1]
		}
	case "stripLeadingNonDigits":
		if first := strings.IndexAny(id, "0123456789"); first >= 0 {
			id = id[first:]
		}
	}
	return id
}
