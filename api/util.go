package api

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
)

func maybeUpdateTxtCol[X pgtype.Text | string](cur X, patch nullable.Nullable[string]) X {
	if !patch.IsSpecified() {
		return cur
	}
	switch any(cur).(type) {
	case pgtype.Text:
		if patch.IsNull() || !patch.IsSpecified() {
			return any(pgtype.Text{String: "", Valid: false}).(X)
		}
		return any(pgtype.Text{String: patch.MustGet(), Valid: true}).(X)
	default:
		if patch.IsNull() || !patch.IsSpecified() {
			return any("").(X)
		}
		return any(patch.MustGet()).(X)
	}
}

func NlblToPGTxt(nlbl nullable.Nullable[string]) pgtype.Text {
	if nlbl.IsNull() || !nlbl.IsSpecified() {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: nlbl.MustGet(), Valid: true}
}

func NlblToPGUUID(nlbl nullable.Nullable[uuid.UUID]) pgtype.UUID {
	if nlbl.IsNull() || !nlbl.IsSpecified() {
		return pgtype.UUID{Bytes: [16]byte{}, Valid: false}
	}
	return pgtype.UUID{Bytes: nlbl.MustGet(), Valid: true}
}

func PtrToPGTxt(ptr *string) pgtype.Text {
	if ptr == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: *ptr, Valid: true}
}

func PGTxtToNlbl(pgtxt pgtype.Text) nullable.Nullable[string] {
	if !pgtxt.Valid {
		nlbl := nullable.NewNullNullable[string]()
		nlbl.SetUnspecified() // We don't store explicitly null strings
		return nlbl
	}
	return nullable.NewNullableWithValue(pgtxt.String)
}
