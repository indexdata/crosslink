package api

import (
	"testing"

	"github.com/google/uuid"
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

func TestResolveCombinedSymbol(t *testing.T) {
	a, s, e := resolveCombinedSymbol("AUTH:SYM:🤔:BOL")
	if e != nil {
		t.Error("Unexpected error resolving symbol")
	}
	if a != "AUTH" || s != "SYM:🤔:BOL" {
		t.Errorf("failed to resolve symbol, expected authority symbol got authority %s symbol %s", a, s)
	}
}

func TestElementHasProperty(t *testing.T) {
	type S struct {
		A string
		B int
	}
	slice := []S{
		{A: "A", B: 1},
		{A: "B", B: 2},
		{A: "A", B: 3},
	}
	emptySlice := []S{}
	if !elementHasProperty(slice, "A", "B") {
		t.Error("Failed to return true when property present with value")
	}
	if elementHasProperty(slice, "A", "D") {
		t.Error("Failed to return false when property not present with value")
	}
	if elementHasProperty(emptySlice, "A", "B") {
		t.Error("Failed to return false on an empty slice")
	}
	type U struct {
		Id *uuid.UUID
	}
	someId := uuid.Must(uuid.Parse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"))
	idSlice := []U{
		{Id: &someId},
	}
	if !elementHasProperty(idSlice, "Id", someId) {
		t.Error("Failed to return true when property present with UUID value")
	}
	otherId := uuid.Must(uuid.Parse("6ba7b810-9dad-11d1-80b4-000000000000"))
	if elementHasProperty(idSlice, "Id", otherId) {
		t.Error("Failed to return false when property not present with UUID value")
	}
}
