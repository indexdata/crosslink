package api

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
)

func TestMaybeUpdateTxtCol(t *testing.T) {
	pgrep := pgtype.Text{String: "replacement", Valid: true}
	pgcur := pgtype.Text{String: "current", Valid: true}
	pgzero := pgtype.Text{}
	unspecifiedNullable := nullable.NewNullNullable[string]()
	unspecifiedNullable.SetUnspecified()
	if maybeUpdateTxtCol("current", nullable.NewNullableWithValue("replacement")) != "replacement" {
		t.Error("failed on string input when replacement present")
	}
	if maybeUpdateTxtCol(pgcur, nullable.NewNullableWithValue("replacement")) != pgrep {
		t.Error("failed on pgtype input when replacement present")
	}
	if maybeUpdateTxtCol("current", nullable.NewNullNullable[string]()) != "" {
		t.Error("failed on string input when replacement null")
	}
	if maybeUpdateTxtCol(pgcur, nullable.NewNullNullable[string]()) != pgzero {
		t.Error("failed on pgtype input when replacement null")
	}
	if maybeUpdateTxtCol("current", unspecifiedNullable) != "current" {
		t.Error("failed on string input when replacement unspecified")
	}
}
