package pr_db

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notificationListRepo applies the query's kind filters and limit to an
// in-memory list ordered oldest first.
type notificationListRepo struct {
	PrRepo
	notifications []Notification
	failOn        func(GetNotificationsByPrIdParams) bool
	params        []GetNotificationsByPrIdParams
}

func (r *notificationListRepo) GetNotificationsByPrId(_ common.ExtendedContext, params GetNotificationsByPrIdParams) ([]Notification, int64, error) {
	r.params = append(r.params, params)
	if r.failOn != nil && r.failOn(params) {
		return nil, 0, errors.New("db down")
	}
	matched := []Notification{}
	for _, n := range r.notifications {
		if params.Kind != "" && string(n.Kind) != params.Kind {
			continue
		}
		if params.ExcludeKind != "" && string(n.Kind) == params.ExcludeKind {
			continue
		}
		matched = append(matched, n)
	}
	full := int64(len(matched))
	if len(matched) > int(params.Limit) {
		matched = matched[:params.Limit]
	}
	return matched, full, nil
}

func ids(list []Notification) []string {
	out := []string{}
	for _, n := range list {
		out = append(out, n.ID)
	}
	return out
}

func TestGetTemplateNotifications(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	repo := &notificationListRepo{notifications: []Notification{
		{ID: "note", Kind: NotificationKindNote},
		{ID: "condition", Kind: NotificationKindCondition},
		{ID: "cancel", Kind: NotificationKindCancellationReason},
		{ID: "unfilled", Kind: NotificationKindUnfilledReason},
	}}

	notes, conditions, err := GetTemplateNotifications(ctx, repo, "pr-1")

	require.NoError(t, err)
	assert.Equal(t, []GetNotificationsByPrIdParams{
		{Limit: templateNotificationLimit, PrID: "pr-1", ExcludeKind: string(NotificationKindCondition)},
		{Limit: templateNotificationLimit, PrID: "pr-1", Kind: string(NotificationKindCondition)},
	}, repo.params)
	assert.Equal(t, []string{"note", "cancel", "unfilled"}, ids(notes))
	assert.Equal(t, []string{"condition"}, ids(conditions))
}

func TestGetTemplateNotificationsNotesCannotCrowdOutConditions(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	repo := &notificationListRepo{}
	for i := range templateNotificationLimit + 1 {
		repo.notifications = append(repo.notifications, Notification{ID: fmt.Sprintf("note-%d", i), Kind: NotificationKindNote})
	}
	repo.notifications = append(repo.notifications, Notification{ID: "late-condition", Kind: NotificationKindCondition})

	notes, conditions, err := GetTemplateNotifications(ctx, repo, "pr-1")

	require.NoError(t, err)
	assert.Len(t, notes, templateNotificationLimit)
	assert.Equal(t, []string{"late-condition"}, ids(conditions))
}

func TestGetTemplateNotificationsErrors(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	for _, tt := range []struct {
		name   string
		failOn func(GetNotificationsByPrIdParams) bool
		want   string
	}{
		{"staff notes", func(p GetNotificationsByPrIdParams) bool { return p.ExcludeKind != "" }, "load staff notes for pr-1: db down"},
		{"loan conditions", func(p GetNotificationsByPrIdParams) bool { return p.Kind != "" }, "load loan conditions for pr-1: db down"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := GetTemplateNotifications(ctx, &notificationListRepo{failOn: tt.failOn}, "pr-1")
			assert.EqualError(t, err, tt.want)
		})
	}
}
