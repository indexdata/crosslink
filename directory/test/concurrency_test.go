package test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

var consortiumPermissionHeaders = map[string]string{
	"X-Okapi-Tenant":      "ANINST",
	"X-Okapi-Permissions": `["directory.consortium.all"]`,
}

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
				`{"symbols":[{"authority":"TEST","symbol":"CONCURRENT1"}]}`, consortiumPermissionHeaders)
		}()

		// PATCH 2: Update description
		go func() {
			defer wg.Done()
			res2, data2 = jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000002",
				`{"description":"Updated concurrently"}`, consortiumPermissionHeaders)
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
		_, data := jsonReq(t, http.MethodGet, "/entries/by-id/00000000-0000-0000-0000-000000000002", "", consortiumPermissionHeaders)
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
				`{"name":"Updated"}`, consortiumPermissionHeaders)
		}()

		// DELETE: Remove same entry
		go func() {
			defer wg.Done()
			deleteRes, _ = jsonReq(t, http.MethodDelete, "/entries/by-id/00000000-0000-0000-0000-000000000001", "", consortiumPermissionHeaders)
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

	t.Run("ConcurrentConsortiumPosts", func(t *testing.T) {
		resetDb()
		_, err := dbpool.Exec(context.Background(), "UPDATE entries SET parent = NULL, type = 'Institution'")
		if err != nil {
			t.Fatalf("failed to prepare entries: %v", err)
		}

		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for _, name := range []string{"Consortium One", "Consortium Two"} {
			name := name
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, _ := jsonReq(t, http.MethodPost, "/entries", `{"name":"`+name+`","type":"Consortium"}`, consortiumPermissionHeaders)
				statuses <- res.StatusCode
			}()
		}
		wg.Wait()
		close(statuses)

		assertOneConsortiumWrite(t, statuses, http.StatusCreated)
	})

	t.Run("ConcurrentConsortiumPromotions", func(t *testing.T) {
		resetDb()
		_, err := dbpool.Exec(context.Background(), "UPDATE entries SET parent = NULL, type = 'Institution'")
		if err != nil {
			t.Fatalf("failed to prepare entries: %v", err)
		}

		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for _, id := range []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"} {
			id := id
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, _ := jsonReq(t, http.MethodPatch, "/entries/by-id/"+id, `{"type":"Consortium"}`, consortiumPermissionHeaders)
				statuses <- res.StatusCode
			}()
		}
		wg.Wait()
		close(statuses)

		assertOneConsortiumWrite(t, statuses, http.StatusNoContent)
	})
}

func assertOneConsortiumWrite(t *testing.T, statuses <-chan int, successStatus int) {
	t.Helper()
	successes, rejections := 0, 0
	for status := range statuses {
		switch status {
		case successStatus:
			successes++
		case http.StatusBadRequest:
			rejections++
		default:
			t.Errorf("unexpected response status: %d", status)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Errorf("expected one success and one rejection, got %d successes and %d rejections", successes, rejections)
	}

	var count int
	if err := dbpool.QueryRow(context.Background(), "SELECT COUNT(*) FROM entries WHERE type = 'Consortium'").Scan(&count); err != nil {
		t.Fatalf("failed to count consortium entries: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly one consortium entry, got %d", count)
	}
}
