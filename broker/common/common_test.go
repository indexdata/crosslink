package common

import (
	"reflect"
	"testing"
)

type User struct {
	ID     int
	Name   string
	Active bool
}

func TestStructToMap(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name:  "Basic struct conversion",
			input: User{ID: 1, Name: "Alice", Active: true},
			want: map[string]interface{}{
				"ID":     1,
				"Name":   "Alice",
				"Active": true,
			},
			wantErr: false,
		},
		{
			name:  "Pointer to struct",
			input: &User{ID: 2, Name: "Bob", Active: false},
			want: map[string]interface{}{
				"ID":     2,
				"Name":   "Bob",
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
