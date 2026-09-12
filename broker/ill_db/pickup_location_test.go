package ill_db

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	dirapi "github.com/indexdata/crosslink/directory/api"
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
