package api

import (
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
)

func TestMaybeUpdatePatronProfiles(t *testing.T) {
	current := []byte(`[{"code":"CURRENT","name":"Current","canCreateRequests":true}]`)

	var unspecified nullable.Nullable[PatronProfiles]
	assert.Equal(t, current, maybeUpdatePatronProfiles(current, unspecified))
	assert.Nil(t, maybeUpdatePatronProfiles(current, nullable.NewNullNullable[PatronProfiles]()))

	replacement := PatronProfiles{{Code: "BLOCKED", Name: "Blocked patrons", CanCreateRequests: false}}
	assert.JSONEq(t,
		`[{"code":"BLOCKED","name":"Blocked patrons","canCreateRequests":false}]`,
		string(maybeUpdatePatronProfiles(current, nullable.NewNullableWithValue(replacement))),
	)

	empty := PatronProfiles{}
	assert.Equal(t, []byte(`[]`), maybeUpdatePatronProfiles(current, nullable.NewNullableWithValue(empty)))
}
