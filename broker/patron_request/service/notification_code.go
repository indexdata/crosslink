package prservice

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5/pgtype"
)

// notificationCodeLists selects the Directory vocabulary that defines codes of each kind.
var notificationCodeLists = map[pr_db.NotificationKind]func(dirapi.IllConfig) *[]dirapi.IllConfigCodeDescription{
	pr_db.NotificationKindCondition:          func(c dirapi.IllConfig) *[]dirapi.IllConfigCodeDescription { return c.LoanConditions },
	pr_db.NotificationKindCancellationReason: func(c dirapi.IllConfig) *[]dirapi.IllConfigCodeDescription { return c.CancellationReasons },
	pr_db.NotificationKindUnfilledReason:     func(c dirapi.IllConfig) *[]dirapi.IllConfigCodeDescription { return c.UnfilledReasons },
}

// notificationCodeDescription snapshots the description that ownerSymbol's
// Directory entry gives code for kind. Codes belong to the sending library's
// vocabulary, so ownerSymbol must be the notification's from symbol. Behind an
// opaque broker that symbol is the broker's, which defines no vocabulary, so
// such notifications get no description. Only the local peer cache is read, so
// storing a notification never waits on Directory; any miss stores no
// description.
func notificationCodeDescription(ctx common.ExtendedContext, illRepo ill_db.IllRepo, ownerSymbol string, kind pr_db.NotificationKind, code pgtype.Text) pgtype.Text {
	field, ok := notificationCodeLists[kind]
	if !ok || !code.Valid || code.String == "" || illRepo == nil {
		return pgtype.Text{}
	}
	peer, err := illRepo.GetPeerBySymbol(ctx, ownerSymbol)
	if err != nil {
		ctx.Logger().Debug("no cached peer for notification code description", "symbol", ownerSymbol, "kind", kind, "error", err)
		return pgtype.Text{}
	}
	if desc := common.IllConfigCodeDescription(peer.CustomData, code.String, field); desc != "" {
		return getDbText(desc)
	}
	return pgtype.Text{}
}
