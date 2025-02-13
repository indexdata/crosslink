package api

import (
	"testing"

	"github.com/oapi-codegen/nullable"
)

func TestMaybeUpdateTxtCol(t *testing.T) {
	unspecifiedNullable := nullable.NewNullNullable[string]()
	unspecifiedNullable.SetUnspecified()
	cur := "current"
	if *maybeUpdateTxtCol(&cur, nullable.NewNullableWithValue("replacement")) != "replacement" {
		t.Error("failed on string input when replacement present")
	}
	if maybeUpdateTxtCol(&cur, nullable.NewNullNullable[string]()) != nil {
		t.Error("failed on string input when replacement null")
	}
	if *maybeUpdateTxtCol(&cur, unspecifiedNullable) != "current" {
		t.Error("failed on string input when replacement unspecified")
	}
}
