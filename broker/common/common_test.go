package common

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

type User struct {
	ID     int     `json:"id"`
	Name   *string `json:"name,omitempty"`
	Active bool
}

var bob = "Bob"
var alice = "Alice"

func TestStructToMap(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name:  "Basic struct conversion",
			input: User{ID: 1, Name: &alice, Active: true},
			want: map[string]interface{}{
				"id":     1,
				"name":   &alice,
				"Active": true,
			},
			wantErr: false,
		},
		{
			name:  "Pointer to struct",
			input: &User{ID: 2, Name: &bob, Active: false},
			want: map[string]interface{}{
				"id":     2,
				"name":   &bob,
				"Active": false,
			},
			wantErr: false,
		},
		{
			name:    "Error on non-struct (int)",
			input:   42,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Error on non-struct (slice)",
			input:   []string{"a", "b"},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := StructToMap(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("StructToMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StructToMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetItemParams(t *testing.T) {
	// Just ID
	note := MULTIPLE_ITEMS + "\n1\n" + MULTIPLE_ITEMS_END
	result, startIdx, endIdx := GetItemParams(note)
	assert.Equal(t, 0, startIdx)
	assert.Equal(t, 18, endIdx)
	assert.Equal(t, [][]string{{"1"}}, result)

	// All params
	note = MULTIPLE_ITEMS + "\n1|2\\||3\\\\\n" + MULTIPLE_ITEMS_END
	result, startIdx, endIdx = GetItemParams(note)
	assert.Equal(t, 0, startIdx)
	assert.Equal(t, 26, endIdx)
	assert.Equal(t, [][]string{{"1", "2|", "3\\"}}, result)

	// Incorrect tag order
	note = MULTIPLE_ITEMS_END + "\n1\n" + MULTIPLE_ITEMS
	result, startIdx, endIdx = GetItemParams(note)
	assert.Equal(t, 21, startIdx)
	assert.Equal(t, 0, endIdx)
	assert.Nil(t, result)
}
