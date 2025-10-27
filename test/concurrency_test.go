package test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

func TestConcurrency(t *testing.T) {
	t.Run("ConcurrentEntryPatch", func(t *testing.T) {
		resetDb()

		var wg sync.WaitGroup
		wg.Add(2)

		var res1, res2 *http.Response
		var data1, data2 string

		// PATCH 1: Update symbols
		go func() {
			defer wg.Done()
			res1, data1 = jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000002",
				`{"symbols":[{"authority":"TEST","symbol":"CONCURRENT1"}]}`)
		}()

		// PATCH 2: Update description
		go func() {
			defer wg.Done()
			res2, data2 = jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000002",
				`{"description":"Updated concurrently"}`)
		}()

		wg.Wait()

		// Both should succeed
		if res1.StatusCode != http.StatusNoContent {
			t.Errorf("PATCH 1 failed: %d %s", res1.StatusCode, data1)
		}
		if res2.StatusCode != http.StatusNoContent {
			t.Errorf("PATCH 2 failed: %d %s", res2.StatusCode, data2)
		}

		// Verify both updates were applied (without FOR UPDATE, one will be lost)
		_, data := jsonReq(t, http.MethodGet, "/entries/by-id/00000000-0000-0000-0000-000000000002", "")
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			t.Fatalf("Failed to unmarshal entry: %v", err)
		}

		if desc, _ := entry["description"].(string); desc != "Updated concurrently" {
			t.Errorf("Description lost in race: got %v", entry["description"])
		}
		if symbols, ok := entry["symbols"].([]interface{}); !ok || len(symbols) == 0 {
			t.Errorf("Symbols lost in race: got %v", entry["symbols"])
		}
	})

	t.Run("EntryPatchDeleteRace", func(t *testing.T) {
		resetDb()

		var wg sync.WaitGroup
		wg.Add(2)

		var patchRes *http.Response
		var deleteRes *http.Response

		// PATCH: Update entry
		go func() {
			defer wg.Done()
			patchRes, _ = jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000001",
				`{"name":"Updated"}`)
		}()

		// DELETE: Remove same entry
		go func() {
			defer wg.Done()
			deleteRes, _ = jsonReq(t, http.MethodDelete, "/entries/by-id/00000000-0000-0000-0000-000000000001", "")
		}()

		wg.Wait()

		// One should succeed, but without locking, behavior is undefined
		// With FOR UPDATE, they will serialize properly
		if patchRes.StatusCode == http.StatusNoContent && deleteRes.StatusCode == http.StatusNoContent {
			// Both succeeded - verify final state is consistent
			var count int
			err := dbpool.QueryRow(context.Background(),
				"SELECT COUNT(*) FROM entries WHERE id = '00000000-0000-0000-0000-000000000001'").Scan(&count)
			if err != nil || count != 0 {
				t.Error("Entry should be deleted if DELETE succeeded")
			}
		}
	})
}
