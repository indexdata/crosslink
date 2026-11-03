package pr_db

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
)

// templateNotificationLimit bounds each category of notifications loaded to
// render one email or pull slip. Categories are loaded separately so staff
// notes can never crowd loan conditions out of the rendered output.
const templateNotificationLimit = 100

// GetTemplateNotifications loads a request's staff notes and loan conditions
// for rendering. Every kind other than condition counts as a note, so the note
// text of reason notifications still reaches {{.StaffNotes}}; the reasons
// themselves are rendered elsewhere.
func GetTemplateNotifications(ctx common.ExtendedContext, repo PrRepo, prID string) (notes []Notification, conditions []Notification, err error) {
	notes, _, err = repo.GetNotificationsByPrId(ctx, GetNotificationsByPrIdParams{
		Limit: templateNotificationLimit, PrID: prID, ExcludeKind: string(NotificationKindCondition),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("load staff notes for %s: %w", prID, err)
	}
	conditions, _, err = repo.GetNotificationsByPrId(ctx, GetNotificationsByPrIdParams{
		Limit: templateNotificationLimit, PrID: prID, Kind: string(NotificationKindCondition),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("load loan conditions for %s: %w", prID, err)
	}
	return notes, conditions, nil
}
