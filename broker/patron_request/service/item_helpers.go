package prservice

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/jackc/pgx/v5/pgtype"
)

var errNoItemsFound = errors.New("no items found for patron request")

func ensureFallbackRequesterItem(ctx common.ExtendedContext, repo pr_db.PrRepo, pr pr_db.PatronRequest) (pr_db.Item, error) {
	items, err := repo.GetItemsByPrId(ctx, pr.ID)
	if err != nil {
		return pr_db.Item{}, fmt.Errorf("failed to check existing requester items: %w", err)
	}
	if len(items) > 0 {
		return items[0], nil
	}

	title := pr.IllRequest.BibliographicInfo.Title
	return repo.SaveItem(ctx, pr_db.SaveItemParams{
		ID:        uuid.NewSHA1(uuid.NameSpaceOID, []byte("crosslink:fallback-requester-item:"+pr.ID)).String(),
		CreatedAt: pgtype.Timestamp{Valid: true, Time: time.Now()},
		PrID:      pr.ID,
		Title:     getDbTextPtr(&title),
		Barcode:   requesterItemBarcode(pr.ID, 0, 1),
	})
}

func requesterItemBarcode(prID string, index int, itemCount int) string {
	if itemCount == 1 {
		return prID
	}
	return fmt.Sprintf("%s-%d", prID, index+1)
}
