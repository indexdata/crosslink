package importoapi

import (
	"testing"
	"time"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/require"
)

func TestImportModelsReuseDomainEnums(t *testing.T) {
	side := PatronRequestSide("borrowing")
	_ = ImportPatronRequest{Side: side}
	_ = PatronRequest{Side: side}

	direction := NotificationDirection("sent")
	kind := NotificationKind("note")
	_ = ImportPatronRequestNotification{Direction: direction, Kind: kind}
	_ = PrNotification{Direction: direction, Kind: kind}

	status := LocatedSupplierStatus("new")
	_ = ImportLocatedSupplier{SupplierStatus: &status}
	_ = LocatedSupplier{SupplierStatus: &status}

	for _, value := range []PatronRequestSide{"borrowing", "lending"} {
		require.True(t, value.Valid(), "side %q should be valid", value)
	}
	require.False(t, PatronRequestSide("invalid").Valid())

	for _, value := range []NotificationDirection{"sent", "received"} {
		require.True(t, value.Valid(), "direction %q should be valid", value)
	}
	require.False(t, NotificationDirection("invalid").Valid())

	for _, value := range []NotificationKind{"note", "condition"} {
		require.True(t, value.Valid(), "kind %q should be valid", value)
	}
	require.False(t, NotificationKind("invalid").Valid())

	for _, value := range []LocatedSupplierStatus{"new", "selected", "skipped"} {
		require.True(t, value.Valid(), "supplier status %q should be valid", value)
	}
	require.False(t, LocatedSupplierStatus("invalid").Valid())
}

func TestImportAndResponseReusePatronRequestNotification(t *testing.T) {
	notification := PatronRequestNotification{
		Id:         "notification-1",
		FromSymbol: "ISIL:FROM",
		ToSymbol:   "ISIL:TO",
		Direction:  NotificationDirection("sent"),
		Kind:       NotificationKind("note"),
		CreatedAt:  testNotificationTimestamp(),
	}

	acceptImportPatronRequestNotification(notification)
	acceptPrNotification(notification)
}

func TestImportPatronRequestReusesBibliographicInfo(t *testing.T) {
	info := iso18626.BibliographicInfo{}
	request := ImportPatronRequest{RetryBibInfo: &info}
	require.Same(t, &info, request.RetryBibInfo)
}

func acceptImportPatronRequestNotification(ImportPatronRequestNotification) {}

func acceptPrNotification(PrNotification) {}

func testNotificationTimestamp() time.Time {
	return time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
}
