package ill_db

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestCachedPeerByDirectoryEntryID(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	for _, policy := range []RefreshPolicy{RefreshPolicyTransaction, RefreshPolicyNever} {
		t.Run(string(policy), func(t *testing.T) {
			id := uuid.New()
			entry := dirapi.Entry{Id: &id, Name: "Original branch"}
			peer, err := illRepo.SavePeer(ctx, SavePeerParams{ID: uuid.NewString(), CustomData: entry, Name: entry.Name, RefreshPolicy: policy, RefreshTime: GetPgNow()})
			require.NoError(t, err)
			_, err = illRepo.SaveBranchSymbol(ctx, SaveBranchSymbolParams{PeerID: peer.ID, SymbolValue: "ISIL:" + uuid.NewString()})
			require.NoError(t, err)
			branches, err := illRepo.GetBranchSymbolsByPeerId(ctx, peer.ID)
			require.NoError(t, err)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "/by-id/"+id.String(), r.URL.Path)
				entry.Name = "Updated branch"
				require.NoError(t, json.NewEncoder(w).Encode(entry))
			}))
			defer server.Close()
			da := createDirectoryAdapter(server.URL)
			cached, query, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, da)
			require.NoError(t, err)
			require.Equal(t, peer, cached)
			require.Equal(t, "<cached>", query)
			require.Zero(t, calls)
			peer.RefreshTime = Get10MinsAgo()
			_, err = illRepo.SavePeer(ctx, SavePeerParams(peer))
			require.NoError(t, err)
			refreshed, _, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, da)
			require.NoError(t, err)
			require.Equal(t, peer.ID, refreshed.ID)
			if policy == RefreshPolicyTransaction {
				require.Equal(t, 1, calls)
				require.Equal(t, "Updated branch", refreshed.CustomData.Name)
			} else {
				require.Zero(t, calls)
				require.Equal(t, "Original branch", refreshed.CustomData.Name)
			}
			remaining, err := illRepo.GetBranchSymbolsByPeerId(ctx, peer.ID)
			require.NoError(t, err)
			require.Equal(t, branches, remaining)
		})
	}
}

func TestDirectoryEntryCacheMissAndSymbolReuse(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	id := uuid.New()
	entry := dirapi.Entry{Id: &id, Name: "Branch without symbols"}
	calls := 0
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/by-id/"+id.String() {
			require.NoError(t, json.NewEncoder(w).Encode(entry))
		} else {
			require.NoError(t, json.NewEncoder(w).Encode(dirapi.EntriesResponse{Items: []dirapi.Entry{entry}}))
		}
	}))
	defer server.Close()
	da := createDirectoryAdapter(missing.URL, server.URL)
	first, _, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, da)
	require.NoError(t, err)
	require.Equal(t, entry, first.CustomData)
	second, query, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, da)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "<cached>", query)
	require.Equal(t, 1, calls)
	// A symbol added later must refresh this same peer, rather than creating a duplicate.
	symbol := "branch-" + uuid.NewString()
	require.NoError(t, json.Unmarshal([]byte(`{"symbols":[{"authority":"ISIL","symbol":"`+symbol+`"}]}`), &entry))
	peers, _, err := illRepo.GetCachedPeersBySymbols(ctx, []string{"ISIL:" + symbol}, createDirectoryAdapter(server.URL))
	require.NoError(t, err)
	require.Len(t, peers, 1)
	require.Equal(t, first.ID, peers[0].ID)
	cached, _, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, da)
	require.NoError(t, err)
	require.Equal(t, peers[0], cached)
	require.Equal(t, 2, calls)
}

func TestDirectoryEntryCacheRejectsInvalidResponses(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			id := uuid.New()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				wrongID := uuid.New()
				_ = json.NewEncoder(w).Encode(dirapi.Entry{Id: &wrongID, Name: "Wrong entry"})
			}))
			defer server.Close()
			_, _, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, createDirectoryAdapter(server.URL))
			require.Error(t, err)
		})
	}
}

func TestMockDirectoryPickupLocationCache(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	id := uuid.New()
	directory := &adapter.MockDirectoryLookupAdapter{}
	peer, _, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, directory)
	require.NoError(t, err)
	require.Equal(t, id, *peer.CustomData.Id)
	require.NotEmpty(t, common.DirectoryShippingAddress(peer.CustomData).Line1)
	require.NotEmpty(t, *peer.CustomData.LmsConfig.RequesterPickupLocation)
	cached, query, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, directory)
	require.NoError(t, err)
	require.Equal(t, "<cached>", query)
	require.Equal(t, peer, cached)
}

func TestConcurrentDirectoryPeerCreation(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	id := uuid.New()
	const workers = 8
	var arrived atomic.Int32
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if arrived.Add(1) == workers {
			close(ready)
		}
		select {
		case <-ready:
		case <-r.Context().Done():
			return
		}
		entry := dirapi.Entry{Id: &id, Name: "Concurrent pickup location", Symbols: &[]dirapi.Symbol{{Authority: "ISIL", Symbol: id.String()}}}
		if r.URL.Query().Get("cql") != "" {
			require.NoError(t, json.NewEncoder(w).Encode(dirapi.EntriesResponse{Items: []dirapi.Entry{entry}}))
		} else {
			require.NoError(t, json.NewEncoder(w).Encode(entry))
		}
	}))
	defer server.Close()
	da := createDirectoryAdapter(server.URL)
	type result struct {
		peer Peer
		err  error
	}
	results := make(chan result, workers)
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lookupCtx := common.CreateExtCtxWithArgs(timeoutCtx, nil)
	for worker := range workers {
		go func() {
			if worker%2 == 0 {
				peers, _, err := illRepo.GetCachedPeersBySymbols(lookupCtx, []string{"ISIL:" + id.String()}, da)
				if err == nil && len(peers) != 1 {
					err = errors.New("expected one cached peer")
				}
				peer := Peer{}
				if len(peers) == 1 {
					peer = peers[0]
				}
				results <- result{peer, err}
			} else {
				peer, _, err := illRepo.GetCachedPeerByDirectoryEntryID(lookupCtx, id, da)
				results <- result{peer, err}
			}
		}()
	}
	var peerID string
	for range workers {
		result := <-results
		require.NoError(t, result.err)
		if peerID == "" {
			peerID = result.peer.ID
		}
		require.Equal(t, peerID, result.peer.ID)
	}
	var count int
	err := illRepo.(*PgIllRepo).Pool.QueryRow(ctx, "SELECT count(*) FROM peer WHERE custom_data ->> 'id' = $1", id.String()).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestDeduplicateDirectoryPeersMigration(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	tx, err := illRepo.(*PgIllRepo).Pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "DROP INDEX peer_directory_entry_id_idx; CREATE INDEX peer_directory_entry_id_idx ON peer ((custom_data ->> 'id'))")
	require.NoError(t, err)
	id := uuid.NewString()
	// Equal timestamps exercise the deterministic local-ID tiebreaker.
	_, err = tx.Exec(ctx, `INSERT INTO peer (id, name, refresh_policy, refresh_time, url, vendor, broker_mode, custom_data, loans_count, borrows_count)
 VALUES ('dedup-a', 'Survivor', 'never', '2026-01-01', '', '', '', jsonb_build_object('id', $1::text), 2, 3),
        ('dedup-b', 'Duplicate', 'transaction', '2026-01-01', '', '', '', jsonb_build_object('id', $1::text), 5, 7)`, id)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO symbol VALUES ('dedup-symbol', 'dedup-b'); INSERT INTO branch_symbol VALUES ('dedup-branch', 'dedup-b')`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO ill_transaction (id, timestamp, requester_id, ill_transaction_data)
        VALUES ('dedup-transaction', now(), 'dedup-b', '{}');
        INSERT INTO located_supplier (id, ill_transaction_id, supplier_id, supplier_symbol)
        VALUES ('dedup-supplier', 'dedup-transaction', 'dedup-b', 'dedup-symbol')`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../migrations/064_unique_peer_directory_entry_id.up.sql")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, string(migration))
	require.NoError(t, err)
	var survivor, name, policy string
	var loans, borrows int
	err = tx.QueryRow(ctx, "SELECT id, name, refresh_policy, loans_count, borrows_count FROM peer WHERE custom_data ->> 'id' = $1", id).Scan(&survivor, &name, &policy, &loans, &borrows)
	require.NoError(t, err)
	require.Equal(t, "dedup-a", survivor)
	require.Equal(t, "Survivor", name)
	require.Equal(t, "never", policy)
	require.Equal(t, 7, loans)
	require.Equal(t, 10, borrows)
	for _, table := range []string{"symbol", "branch_symbol"} {
		var peerID string
		err = tx.QueryRow(ctx, "SELECT peer_id FROM "+table+" WHERE peer_id = 'dedup-a'").Scan(&peerID)
		require.NoError(t, err)
	}
	var requesterID, supplierID string
	require.NoError(t, tx.QueryRow(ctx, "SELECT requester_id FROM ill_transaction WHERE id = 'dedup-transaction'").Scan(&requesterID))
	require.NoError(t, tx.QueryRow(ctx, "SELECT supplier_id FROM located_supplier WHERE id = 'dedup-supplier'").Scan(&supplierID))
	require.Equal(t, survivor, requesterID)
	require.Equal(t, survivor, supplierID)
	// The unique index rejects duplicates even outside the cache creation helper.
	_, err = tx.Exec(ctx, `INSERT INTO peer (id, name, refresh_policy, url, vendor, broker_mode, custom_data)
 VALUES ('dedup-c', '', '', '', '', '', jsonb_build_object('id', $1::text))`, id)
	require.Error(t, err)
	require.True(t, errors.As(err, new(*pgconn.PgError)))
	require.Equal(t, "23505", err.(*pgconn.PgError).Code)
}

func TestDirectoryEntryCacheWithReplicas(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	for _, tc := range []struct {
		name     string
		code     string
		street   string
		conflict bool
	}{
		{name: "identical entries", code: "branch-1", street: "1 Library Street"},
		{name: "conflicting pickup codes", code: "branch-2", street: "1 Library Street", conflict: true},
		{name: "conflicting shipping addresses", code: "branch-1", street: "2 Library Street", conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			calls := 0
			replica := func(code, street string) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					require.Equal(t, "/by-id/"+id.String(), r.URL.Path)
					require.NoError(t, json.NewEncoder(w).Encode(dirapi.Entry{
						Id: &id, Name: "Branch",
						LmsConfig: &dirapi.LmsConfig{RequesterPickupLocation: &code},
						Addresses: &[]dirapi.Address{{Type: "Shipping", AddressComponents: &[]dirapi.AddressComponent{{Type: "Thoroughfare", Value: street}}}},
					}))
				}))
			}
			first := replica("branch-1", "1 Library Street")
			defer first.Close()
			second := replica(tc.code, tc.street)
			defer second.Close()
			directory := createDirectoryAdapter(first.URL, second.URL)
			peer, _, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, directory)
			require.Equal(t, 2, calls)
			if tc.conflict {
				require.ErrorContains(t, err, "conflicting responses")
				var count int
				require.NoError(t, illRepo.(*PgIllRepo).Pool.QueryRow(ctx, "SELECT count(*) FROM peer WHERE custom_data ->> 'id' = $1", id.String()).Scan(&count))
				require.Zero(t, count)
				return
			}
			require.NoError(t, err)
			require.Equal(t, id, *peer.CustomData.Id)
			require.Equal(t, "branch-1", *peer.CustomData.LmsConfig.RequesterPickupLocation)
			require.Equal(t, "1 Library Street", common.DirectoryShippingAddress(peer.CustomData).Line1)
			cached, query, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, directory)
			require.NoError(t, err)
			require.Equal(t, "<cached>", query)
			require.Equal(t, peer, cached)
			require.Equal(t, 2, calls)
		})
	}
}

func TestDirectoryPeerInsertConflictSavesSymbols(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	id := uuid.New()
	// A direct-ID lookup wins the insert without any symbol associations.
	winner, _, err := illRepo.GetCachedPeerByDirectoryEntryID(ctx, id, &adapter.MockDirectoryLookupAdapter{})
	require.NoError(t, err)
	symbols, err := illRepo.GetSymbolsByPeerId(ctx, winner.ID)
	require.NoError(t, err)
	require.Empty(t, symbols)

	symbol := "ISIL:" + uuid.NewString()
	branch := "ISIL:" + uuid.NewString()
	// Reproduce a symbol lookup that already observed a cache miss before the winner committed.
	peer, err := illRepo.(*PgIllRepo).createNewPeer(ctx, adapter.DirectoryEntry{
		CustomData:    winner.CustomData,
		Symbols:       []string{symbol},
		BranchSymbols: []string{branch},
	})
	require.NoError(t, err)
	require.Equal(t, winner.ID, peer.ID)
	associated, err := illRepo.GetPeerBySymbol(ctx, symbol)
	require.NoError(t, err)
	require.Equal(t, winner.ID, associated.ID)
	branches, err := illRepo.GetBranchSymbolsByPeerId(ctx, winner.ID)
	require.NoError(t, err)
	require.Equal(t, []BranchSymbol{{SymbolValue: branch, PeerID: winner.ID}}, branches)

	// Subsequent symbol lookups must use the cache without contacting the directory.
	cached, query, err := illRepo.GetCachedPeersBySymbols(ctx, []string{symbol}, nil)
	require.NoError(t, err)
	require.Equal(t, "<cached>", query)
	require.Equal(t, []Peer{winner}, cached)
}
