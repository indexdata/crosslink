package ill_db

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/dbutil"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

var illRepo IllRepo
var dirEntries adapter.EntriesResponse
var respBody []byte

func createDirectoryAdapter(urls ...string) adapter.DirectoryLookupAdapter {
	return adapter.CreateApiDirectory(http.DefaultClient, urls)
}

func TestMain(m *testing.M) {
	ctx, pgc, connStr, err := test.StartPGContainer()
	test.Expect(err, "failed to start db container")
	pgIllRepo := new(PgIllRepo)
	pgIllRepo.Pool, err = dbutil.InitDbPool(connStr)
	test.Expect(err, "failed to create ill repo")
	defer pgIllRepo.Pool.Close()
	_, _, _, err = dbutil.RunMigrateScripts("file://../migrations", connStr)
	test.Expect(err, "failed to run migration scripts")
	illRepo = pgIllRepo
	respBody, err = os.ReadFile("../test/testdata/api-directory-response.json")
	test.Expect(err, "failed to read directory entries file")
	err = json.Unmarshal(respBody, &dirEntries)
	test.Expect(err, "failed to parse directory entries")
	ret := m.Run()
	test.Expect(test.TerminatePGContainer(ctx, pgc), "failed to stop db container")
	os.Exit(ret)
}

func TestGetCachedPeersBySymbol(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(respBody)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	da := createDirectoryAdapter(server.URL)
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	peer0, err := illRepo.SavePeer(ctx, SavePeerParams{ID: "123", Name: "Old ISIL:AU-VUMC peer", RefreshTime: test.GetNow()})
	assert.Equal(t, err, nil)
	_, err = illRepo.SaveSymbol(ctx, SaveSymbolParams{SymbolValue: "ISIL:AU-VUMC", PeerID: peer0.ID})
	assert.Equal(t, err, nil)
	_, err = illRepo.SaveSymbol(ctx, SaveSymbolParams{SymbolValue: "ISIL:AU-VUMC2", PeerID: peer0.ID})
	assert.Equal(t, err, nil)
	_, err = illRepo.SaveBranchSymbol(ctx, SaveBranchSymbolParams{SymbolValue: "ISIL:AU-VUMC-Branch", PeerID: peer0.ID})
	assert.Equal(t, err, nil)
	peer00, _ := illRepo.GetPeerBySymbol(ctx, "ISIL:AU-VUMC")
	assert.Equal(t, peer0, peer00)
	peers, _, _ := illRepo.GetCachedPeersBySymbols(ctx,
		[]string{"ISIL:AU-NALB", "ISIL:AU-VU", "ISIL:AU-VUMC"}, da)
	mapPeers := make(map[string]Peer)
	for _, p := range peers {
		mapPeers[p.ID] = p
	}
	assert.Equal(t, len(peers), 3)
	peer1, _ := illRepo.GetPeerBySymbol(ctx, "ISIL:AU-NALB")
	assert.Equal(t, mapPeers[peer1.ID], peer1)
	peer2, _ := illRepo.GetPeerBySymbol(ctx, "ISIL:AU-VU")
	assert.Equal(t, mapPeers[peer2.ID], peer2)
	peer3, _ := illRepo.GetPeerBySymbol(ctx, "ISIL:AU-VUMC")
	assert.Equal(t, "University of Melbourne / The University of Melbourne: Museums and Collections", peer3.Name)
	assert.Equal(t, mapPeers[peer3.ID], peer3)
	assert.NotEqual(t, peer00, peer3)
	symbols1, _ := illRepo.GetSymbolsByPeerId(ctx, peer1.ID)
	assert.Equal(t, len(symbols1), 1)
	assert.Equal(t, symbols1[0].SymbolValue, "ISIL:AU-NALB")
	symbols2, _ := illRepo.GetSymbolsByPeerId(ctx, peer2.ID)
	assert.Equal(t, len(symbols2), 1)
	assert.Equal(t, symbols2[0].SymbolValue, "ISIL:AU-VU")
	branchSymbols2, _ := illRepo.GetBranchSymbolsByPeerId(ctx, peer2.ID)
	assert.Equal(t, len(branchSymbols2), 2)
	assert.Equal(t, branchSymbols2[0].SymbolValue, "ISIL:AU-VUMC")
	assert.Equal(t, branchSymbols2[1].SymbolValue, "ISIL:AU-VU:M")
	symbols3, _ := illRepo.GetSymbolsByPeerId(ctx, peer3.ID)
	assert.Equal(t, len(symbols3), 1)
	assert.Equal(t, symbols3[0].SymbolValue, "ISIL:AU-VUMC")
	branchSymbols3, _ := illRepo.GetBranchSymbolsByPeerId(ctx, peer3.ID)
	assert.Equal(t, len(branchSymbols3), 0)
}

func TestUpdateCachedPeers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(respBody)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	da := createDirectoryAdapter(server.URL)
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	peer, err := illRepo.SavePeer(ctx, SavePeerParams{ID: "1234", Name: "Old ISIL:NU peer", Vendor: "Alma", BrokerMode: "opaque", RefreshPolicy: "transaction", RefreshTime: Get10MinsAgo()})
	assert.Equal(t, err, nil)
	_, err = illRepo.SaveSymbol(ctx, SaveSymbolParams{SymbolValue: "ISIL:AU-NU", PeerID: peer.ID})
	assert.Equal(t, err, nil)
	peerBefore, _ := illRepo.GetPeerBySymbol(ctx, "ISIL:AU-NU")
	assert.Equal(t, peerBefore, peer)
	assert.Equal(t, "Alma", peerBefore.Vendor)
	assert.Equal(t, "opaque", peerBefore.BrokerMode)
	peers, _, _ := illRepo.GetCachedPeersBySymbols(ctx,
		[]string{"ISIL:AU-NU"}, da)
	assert.Equal(t, len(peers), 1)
	peerCached := peers[0]
	peerAfter, _ := illRepo.GetPeerBySymbol(ctx, "ISIL:AU-NU")
	assert.Equal(t, peerAfter, peerCached)
	assert.Equal(t, "ReShare", peerAfter.Vendor)
	assert.Equal(t, "transparent", peerAfter.BrokerMode)
}

func Get10MinsAgo() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now().UTC().Add(-20 * time.Minute),
		Valid: true,
	}
}

func TestSymCheck(t *testing.T) {
	tests := []struct {
		searchSymbols []string
		foundSymbols  []string
		expected      bool
	}{
		{
			searchSymbols: []string{"abc", "def"},
			foundSymbols:  []string{},
			expected:      false,
		},
		{
			searchSymbols: []string{"a", "b"},
			foundSymbols:  []string{"c", "d"},
			expected:      false,
		},
		{
			searchSymbols: []string{"a", "b"},
			foundSymbols:  []string{"c", "b"},
			expected:      true,
		},
	}
	for _, test := range tests {
		result := symCheck(test.searchSymbols, test.foundSymbols)
		if result != test.expected {
			t.Errorf("symMatch(%v, %v) = %v; expected %v", test.searchSymbols, test.foundSymbols, result, test.expected)
		}
	}
}
