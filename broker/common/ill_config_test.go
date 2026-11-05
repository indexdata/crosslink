package common

import (
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
)

func TestIllConfigCodeDescription(t *testing.T) {
	cancellationReasons := func(c dirapi.IllConfig) *[]dirapi.IllConfigCodeDescription { return c.CancellationReasons }
	reasons := []dirapi.IllConfigCodeDescription{
		{Code: "UserRequest", DefaultDesc: "User requested cancellation"},
		{Code: "LocalCode", DefaultDesc: "Library-defined reason"},
	}
	configured := dirapi.Entry{IllConfig: &dirapi.IllConfig{CancellationReasons: &reasons}}

	tests := []struct {
		name  string
		entry dirapi.Entry
		code  string
		want  string
	}{
		{"default code", configured, "UserRequest", "User requested cancellation"},
		{"custom code", configured, "LocalCode", "Library-defined reason"},
		{"unknown code", configured, "Other", ""},
		{"case sensitive", configured, "userrequest", ""},
		{"empty code", configured, "", ""},
		{"no list", dirapi.Entry{IllConfig: &dirapi.IllConfig{}}, "UserRequest", ""},
		{"no config", dirapi.Entry{}, "UserRequest", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IllConfigCodeDescription(tt.entry, tt.code, cancellationReasons))
		})
	}
}
