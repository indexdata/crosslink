package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidParentForType(t *testing.T) {
	tests := []struct {
		entryType  string
		parentType string
		valid      bool
	}{
		{"Institution", "Consortium", true},
		{"Branch", "Institution", true},
		{"Institution", "Institution", false},
		{"Branch", "Consortium", false},
		{"Consortium", "Consortium", false},
	}
	for _, test := range tests {
		valid, _ := ValidParentForType(test.entryType, test.parentType)
		require.Equal(t, test.valid, valid, "%s -> %s", test.entryType, test.parentType)
	}
}
