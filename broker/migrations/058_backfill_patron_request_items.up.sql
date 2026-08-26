-- Repair patron_request.items caches that were clobbered by a since-fixed bug where
-- UpdatePatronRequest overwrote the trigger-maintained cache with stale in-memory data.
-- A no-op update on every item row re-fires trigger_update_patron_request_items, reusing
-- its aggregation logic instead of duplicating it here.
UPDATE item SET id = id;
