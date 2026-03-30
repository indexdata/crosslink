package common

import (
	"encoding/xml"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

type User struct {
	ID     int     `json:"id"`
	Name   *string `json:"name,omitempty"`
	Active bool
}

type userWithIgnoredField struct {
	Name    string   `json:"name"`
	XMLName xml.Name `json:"-"`
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
				"id":     float64(1),
				"name":   alice,
				"Active": true,
			},
			wantErr: false,
		},
		{
			name:  "Pointer to struct",
			input: &User{ID: 2, Name: &bob, Active: false},
			want: map[string]interface{}{
				"id":     float64(2),
				"name":   bob,
				"Active": false,
			},
			wantErr: false,
		},
		{
			name:  "Skip json dash fields",
			input: userWithIgnoredField{Name: "alice"},
			want: map[string]interface{}{
				"name": "alice",
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

func TestMapToStruct(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]interface{}
		want    User
		wantErr bool
	}{
		{
			name: "Basic map conversion",
			input: map[string]interface{}{
				"id":     float64(1),
				"name":   "Alice",
				"Active": true,
			},
			want:    User{ID: 1, Name: &alice, Active: true},
			wantErr: false,
		},
		{
			name:    "42 as string instead of int",
			input:   map[string]interface{}{"id": "42"},
			want:    User{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got User
			err := MapToStruct(tt.input, &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapToStruct() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapToStruct() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnpackItemsNote(t *testing.T) {
	// Just ID
	note := MULTIPLE_ITEMS + "\n1\n" + MULTIPLE_ITEMS_END
	result, startIdx, endIdx := UnpackItemsNote(note)
	assert.Equal(t, 0, startIdx)
	assert.Equal(t, 18, endIdx)
	assert.Equal(t, [][]string{{"1"}}, result)

	// All params
	note = MULTIPLE_ITEMS + "\n1|2\\||3\\\\\n" + MULTIPLE_ITEMS_END
	result, startIdx, endIdx = UnpackItemsNote(note)
	assert.Equal(t, 0, startIdx)
	assert.Equal(t, 26, endIdx)
	assert.Equal(t, [][]string{{"1", "2|", "3\\"}}, result)

	// Incorrect tag order
	note = MULTIPLE_ITEMS_END + "\n1\n" + MULTIPLE_ITEMS
	result, startIdx, endIdx = UnpackItemsNote(note)
	assert.Equal(t, 21, startIdx)
	assert.Equal(t, 0, endIdx)
	assert.Nil(t, result)
}

func TestPackItemsNote(t *testing.T) {
	items := [][]string{
		{"T1", "CallNumber1", "Barcode1"},
		{"T2", "CallNumber2", "Barcode2"},
		{"Barcode3"},
	}
	note := PackItemsNote(items)
	expected := MULTIPLE_ITEMS + "\nT1|CallNumber1|Barcode1\nT2|CallNumber2|Barcode2\nBarcode3\n" + MULTIPLE_ITEMS_END
	assert.Equal(t, expected, note)
	result, startIdx, endIdx := UnpackItemsNote(note)
	assert.Equal(t, 0, startIdx)
	assert.Equal(t, len(note)-len(MULTIPLE_ITEMS_END), endIdx)
	assert.Equal(t, items, result)
	assert.Equal(t, [][]string{{"T1", "CallNumber1", "Barcode1"}, {"T2", "CallNumber2", "Barcode2"}, {"Barcode3"}}, result)
}
