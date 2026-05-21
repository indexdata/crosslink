package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
)

func TestMaybeUpdateCol(t *testing.T) {
	unspecifiedNullable := nullable.NewNullNullable[string]()
	unspecifiedNullable.SetUnspecified()
	cur := "current"
	if *maybeUpdateCol(&cur, nullable.NewNullableWithValue("replacement")) != "replacement" {
		t.Error("failed on string input when replacement present")
	}
	if maybeUpdateCol(&cur, nullable.NewNullNullable[string]()) != nil {
		t.Error("failed on string input when replacement null")
	}
	if *maybeUpdateCol(&cur, unspecifiedNullable) != "current" {
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

func TestUnmarshalJSONArray(t *testing.T) {
	type TestStruct struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	// Test with valid JSON array
	jsonArray := [][]byte{
		[]byte(`{"id":"1","name":"foo"}`),
		[]byte(`{"id":"2","name":"bar"}`),
	}

	result, err := unmarshalJSONArray[TestStruct](jsonArray)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(result) != 2 {
		t.Errorf("Expected 2 items, got %d", len(result))
	}
	if result[0].ID != "1" || result[0].Name != "foo" {
		t.Errorf("First item incorrect: %+v", result[0])
	}
	if result[1].ID != "2" || result[1].Name != "bar" {
		t.Errorf("Second item incorrect: %+v", result[1])
	}

	// Test with empty array
	emptyArray := [][]byte{}
	result, err = unmarshalJSONArray[TestStruct](emptyArray)
	if err != nil {
		t.Errorf("Expected no error for empty array, got %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Expected 0 items, got %d", len(result))
	}

	// Test with nil array
	result, err = unmarshalJSONArray[TestStruct](nil)
	if err != nil {
		t.Errorf("Expected no error for nil array, got %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil result for nil array, got %+v", result)
	}

	// Test with invalid JSON
	invalidArray := [][]byte{
		[]byte(`{"id":"1","name":"foo"}`),
		[]byte(`{invalid json}`),
	}
	_, err = unmarshalJSONArray[TestStruct](invalidArray)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestSplitQuery(t *testing.T) {
	qString1 := "dog=woof"
	field1, val1, err := SplitQuery(&qString1)
	if err != nil || *field1 != "dog" || *val1 != "woof" {
		t.Error("Expected 'dog' and 'woof")
	}

	qString2 := "cat=meow=wow"
	field2, val2, err := SplitQuery(&qString2)
	if err != nil || *field2 != "cat" || *val2 != "meow=wow" {
		t.Error("Expected 'cat' and 'meow=wow")
	}

	qString3 := "mouse"
	field3, val3, err := SplitQuery(&qString3)
	if err == nil || field3 != nil || val3 != nil {
		t.Error("Expected error")
	}
}
