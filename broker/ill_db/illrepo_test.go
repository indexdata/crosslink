package ill_db

import "testing"

func TestSymCheck(t *testing.T) {
	tests := []struct {
		searchSymbols []string
		foundSymbols  []string
		expected      bool
	}{
		{
			searchSymbols: []string{"abc", "def"},
			foundSymbols:  []string{},
			expected:      false,
		},
		{
			searchSymbols: []string{"a", "b"},
			foundSymbols:  []string{"c", "d"},
			expected:      false,
		},
		{
			searchSymbols: []string{"a", "b"},
			foundSymbols:  []string{"c", "b"},
			expected:      true,
		},
	}
	for _, test := range tests {
		result := symCheck(test.searchSymbols, test.foundSymbols)
		if result != test.expected {
			t.Errorf("symMatch(%v, %v) = %v; expected %v", test.searchSymbols, test.foundSymbols, result, test.expected)
		}
	}
}
