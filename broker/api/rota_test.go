package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManualRotaTenantGlobs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		patterns []string
		tenant   string
		enabled  bool
	}{
		{"prefix this", []string{"dk-*"}, "dk-this", true},
		{"prefix that", []string{"dk-*"}, "dk-that", true},
		{"different prefix", []string{"dk-*"}, "us-this", false},
		{"full match", []string{"dk-*"}, "other-dk-this", false},
		{"case sensitive", []string{"dk-*"}, "DK-this", false},
		{"all", []string{"*"}, "us-this", true},
		{"missing tenant", []string{"*"}, "", false},
		{"exact", []string{"dk-this"}, "dk-this", true},
		{"exact mismatch", []string{"dk-this"}, "dk-that", false},
		{"multiple trimmed", []string{" dk-* ", " us-* "}, "us-this", true},
		{"single character", []string{"dk-?"}, "dk-a", true},
		{"single character mismatch", []string{"dk-?"}, "dk-ab", false},
		{"character class", []string{"dk-[a-c]"}, "dk-b", true},
		{"character class mismatch", []string{"dk-[a-c]"}, "dk-d", false},
		{"negated class", []string{"dk-[^a]"}, "dk-b", true},
		{"malformed", []string{"dk-["}, "dk-a", false},
		{"malformed before valid", []string{"dk-[", "dk-*"}, "dk-a", true},
		{"malformed after wildcard", []string{"*["}, "dk-a", false},
		{"empty entries", []string{"", " "}, "dk-a", false},
		{"disabled", nil, "dk-a", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := ApiHandler{}
			handler.ConfigureManualRota(nil, tc.patterns)
			assert.Equal(t, tc.enabled, handler.manualRotaEnabled(tc.tenant))
			// Reconfiguration must replace the previous allowlist.
			handler.ConfigureManualRota(nil, nil)
			assert.False(t, handler.manualRotaEnabled(tc.tenant))
		})
	}
}
