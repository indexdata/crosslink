package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestEntryNetworkPriority(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	const network = "20000000-0000-0000-0000-000000000001"
	const entry = "00000000-0000-0000-0000-000000000002"
	routes := []struct{ path, body string }{
		{"/entry-networks", `{"entry":"` + entry + `","network":"` + network + `"`},
		{"/entries/by-id/" + entry + "/networks", `{"id":"` + network + `"`},
		{"/entries/by-symbol/TEST:ANINST/networks", `{"id":"` + network + `"`},
	}
	for _, route := range routes {
		t.Run(route.path, func(t *testing.T) {
			for _, value := range []string{"", ",\"priority\":-8", ",\"priority\":2147483647", ",\"priority\":-2147483648"} {
				res, data := jsonReq(t, http.MethodPost, route.path, route.body+value+"}", headers)
				if res.StatusCode != http.StatusCreated {
					t.Fatalf("create: %d %s", res.StatusCode, data)
				}
				var created struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal([]byte(data), &created); err != nil {
					t.Fatal(err)
				}
				res, data = jsonReq(t, http.MethodGet, "/entry-networks/"+created.ID, "", headers)
				var membership map[string]any
				if err := json.Unmarshal([]byte(data), &membership); err != nil {
					t.Fatal(err)
				}
				expected := float64(0)
				switch value {
				case ",\"priority\":-8":
					expected = -8
				case ",\"priority\":2147483647":
					expected = 2147483647
				case ",\"priority\":-2147483648":
					expected = -2147483648
				}
				if res.StatusCode != http.StatusOK || membership["priority"] != expected {
					t.Fatalf("membership: %d %s, expected priority %v", res.StatusCode, data, expected)
				}
			}
			for _, value := range []string{"1.5", "2147483648", "-2147483649"} {
				res, data := jsonReq(t, http.MethodPost, route.path, route.body+`,"priority":`+value+"}", headers)
				if res.StatusCode != http.StatusBadRequest {
					t.Fatalf("invalid priority %s: %d %s", value, res.StatusCode, data)
				}
			}
		})
	}
	for _, path := range []string{"/entry-networks", "/entry-networks?q=network=" + network} {
		res, data := jsonReq(t, http.MethodGet, path, "", headers)
		var result struct {
			Items []struct {
				Entry    string `json:"entry"`
				Network  string `json:"network"`
				Priority int32  `json:"priority"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(data), &result); err != nil {
			t.Fatal(err)
		}
		first, second := false, false
		for _, item := range result.Items {
			if item.Network == network {
				if item.Entry == "00000000-0000-0000-0000-000000000001" && item.Priority == 7 {
					first = true
				}
				if item.Entry == entry && item.Priority == -8 {
					second = true
				}
			}
		}
		if res.StatusCode != http.StatusOK || !first || !second {
			t.Fatalf("independent priorities missing: %s", data)
		}
	}
	resetDb()
	res, data := jsonReq(t, http.MethodPost, "/entry-networks", `{"entry":"`+entry+`","network":"`+network+`","priority":-8}`, headers)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create independent membership: %d %s", res.StatusCode, data)
	}
	for _, path := range []string{"/entries", "/entries/by-id/00000000-0000-0000-0000-000000000001", "/entries/by-id/" + entry} {
		res, data := jsonReq(t, http.MethodGet, path, "", headers)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.StatusCode, data)
		}
		type embeddedEntry struct {
			ID       string `json:"id"`
			Networks []struct {
				ID       string `json:"id"`
				Priority *int32 `json:"priority"`
			} `json:"networks"`
		}
		var entries []embeddedEntry
		if path == "/entries" {
			var response struct {
				Items []embeddedEntry `json:"items"`
			}
			if err := json.Unmarshal([]byte(data), &response); err != nil {
				t.Fatal(err)
			}
			entries = response.Items
		} else {
			var response embeddedEntry
			if err := json.Unmarshal([]byte(data), &response); err != nil {
				t.Fatal(err)
			}
			entries = []embeddedEntry{response}
		}
		found := 0
		for _, e := range entries {
			for _, n := range e.Networks {
				if n.ID != network {
					continue
				}
				want := int32(7)
				if e.ID == entry {
					want = -8
				}
				if n.Priority == nil || *n.Priority != want {
					t.Fatalf("%s: incorrect membership priority: %s", path, data)
				}
				found++
			}
		}
		wantCount := 1
		if path == "/entries" {
			wantCount = 2
		}
		if found != wantCount {
			t.Fatalf("%s: expected %d memberships, got %d", path, wantCount, found)
		}
	}

	for _, path := range []string{"/networks", "/networks/" + network, "/entries/by-id/00000000-0000-0000-0000-000000000001/networks", "/entries/by-id/00000000-0000-0000-0000-000000000004/networks"} {
		res, data := jsonReq(t, http.MethodGet, path, "", headers)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.StatusCode, data)
		}
		var result any
		if err := json.Unmarshal([]byte(data), &result); err != nil {
			t.Fatal(err)
		}
		assertNoNetworkPriority(t, result)
	}
}

func assertNoNetworkPriority(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		if _, exists := value["priority"]; exists {
			t.Fatalf("unexpected priority in Network response: %#v", value)
		}
		for _, child := range value {
			assertNoNetworkPriority(t, child)
		}
	case []any:
		for _, child := range value {
			assertNoNetworkPriority(t, child)
		}
	}
}

func TestEntryNetworkPriorityMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(sql string) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE SCHEMA priority_migration_test; SET LOCAL search_path TO priority_migration_test")
	apply := func(pattern string) {
		t.Helper()
		paths, err := filepath.Glob("../migrations/" + pattern)
		if err != nil || len(paths) == 0 {
			t.Fatalf("migration lookup %s: %v", pattern, err)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			exec(string(data))
		}
	}
	for version := 1; version <= 6; version++ {
		apply(fmt.Sprintf("%03d_*.up.sql", version))
	}
	exec(`INSERT INTO entries (id,name,type) VALUES ('00000000-0000-0000-0000-000000000001','Consortium','consortium'), ('00000000-0000-0000-0000-000000000002','Library','institution');
 INSERT INTO networks (id,consortium,priority) VALUES ('20000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000001',7), ('20000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001',9);
 INSERT INTO entry_networks (entry,network) SELECT id,'20000000-0000-0000-0000-000000000001' FROM entries;`)
	apply("007_*.up.sql")
	assertCount := func(query string, want int) {
		t.Helper()
		var got int
		if err := tx.QueryRow(ctx, query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s: got %d, want %d", query, got, want)
		}
	}
	assertCount("SELECT count(*) FROM entry_networks WHERE priority=7", 2)
	assertCount("SELECT count(*) FROM information_schema.columns WHERE table_schema='priority_migration_test' AND table_name='networks' AND column_name='priority'", 0)
	apply("007_*.down.sql")
	assertCount("SELECT count(*) FROM networks WHERE priority=7", 1)
	assertCount("SELECT count(*) FROM networks WHERE priority=0", 1)
	apply("007_*.up.sql")
	exec("UPDATE entry_networks SET priority=3 WHERE entry='00000000-0000-0000-0000-000000000002'")
	nested, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../migrations/007_entry_network_priority.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, err = nested.Exec(ctx, string(down))
	if err == nil {
		t.Fatal("rollback should reject differing priorities")
	}
	if err := nested.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertCount("SELECT count(*) FROM entry_networks WHERE priority=3", 1)
	assertCount("SELECT count(*) FROM information_schema.columns WHERE table_schema='priority_migration_test' AND table_name='networks' AND column_name='priority'", 0)
	exec(`INSERT INTO entry_networks (entry,network) VALUES ('00000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000002')`)
	assertCount("SELECT count(*) FROM entry_networks WHERE priority=0", 1)
	// Validate the sample in a separate, freshly migrated schema.
	exec("CREATE SCHEMA priority_sample_test; SET LOCAL search_path TO priority_sample_test")
	apply("*.up.sql")
	sample, err := os.ReadFile("../sampledata.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(sample))
	assertCount("SELECT count(DISTINCT priority) FROM entry_networks", 2)
	assertCount("SELECT count(DISTINCT network) FROM entry_networks", 1)
}
