package pr_db

// ArchiveResult describes an atomic patron-request archive attempt. A skipped
// request reports only the first blocking check, even if other blockers exist.
type ArchiveResult string

const (
	// ArchiveResultArchived means the request was stored and removed from live data.
	ArchiveResultArchived ArchiveResult = "archived"
	// ArchiveResultActiveEvents means an event is NEW/PROCESSING or has a pending signal.
	ArchiveResultActiveEvents ArchiveResult = "active-events-or-signals"
	// ArchiveResultRelatedRequests means an unfinished lending request or Retry
	// successor still depends on the borrowing request's transaction.
	ArchiveResultRelatedRequests ArchiveResult = "unfinished-related-requests"
	// ArchiveResultChangedOrBusy means the selected request no longer matches, or
	// a related request or item could not be locked immediately.
	ArchiveResultChangedOrBusy ArchiveResult = "changed-or-busy"
)
