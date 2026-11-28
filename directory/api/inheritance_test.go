package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClosure(t *testing.T, id string, entry uuid.UUID, start string) Closure {
	t.Helper()
	parsedID := uuid.MustParse(id)
	date, err := time.Parse(time.DateOnly, start)
	require.NoError(t, err)
	return Closure{
		Id:        &parsedID,
		Entry:     entry,
		StartDate: openapi_types.Date{Time: date},
		EndDate:   openapi_types.Date{Time: date},
		Reason:    id,
	}
}

func TestMergeClosures(t *testing.T) {
	branch, institution, consortium := uuid.New(), uuid.New(), uuid.New()
	own := testClosure(t, "00000000-0000-0000-0000-000000000003", branch, "2026-03-01")
	parent := testClosure(t, "00000000-0000-0000-0000-000000000002", institution, "2026-02-01")
	grandparent := testClosure(t, "00000000-0000-0000-0000-000000000001", consortium, "2026-01-01")
	sameDayLowID := testClosure(t, "00000000-0000-0000-0000-000000000004", consortium, "2026-03-01")
	sameDayHighID := testClosure(t, "00000000-0000-0000-0000-000000000005", branch, "2026-03-01")

	tests := []struct {
		name  string
		lists [][]Closure
		want  []Closure
	}{
		{name: "empty", lists: [][]Closure{nil, nil}, want: nil},
		{name: "own only", lists: [][]Closure{{own}}, want: []Closure{own}},
		{
			name:  "own, parent and grandparent ordered by start date",
			lists: [][]Closure{{own}, {parent}, {grandparent}},
			want:  []Closure{grandparent, parent, own},
		},
		{
			name:  "inherited only",
			lists: [][]Closure{nil, {parent}, {grandparent}},
			want:  []Closure{grandparent, parent},
		},
		{
			name:  "same start date ordered by id, duplicates kept",
			lists: [][]Closure{{sameDayHighID}, nil, {sameDayLowID, own}},
			want:  []Closure{own, sameDayLowID, sameDayHighID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mergeClosures(tt.lists...))
		})
	}
}
