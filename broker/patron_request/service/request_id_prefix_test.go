package prservice

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func peerWithRequestIdPrefix(prefix *string) ill_db.Peer {
	return ill_db.Peer{CustomData: dirapi.Entry{IllConfig: &dirapi.IllConfig{RequestIdPrefix: prefix}}}
}

func TestRequestIdPrefix(t *testing.T) {
	configured, empty := "ABC", ""
	for _, tc := range []struct {
		name      string
		peers     []ill_db.Peer
		lookupErr error
		want      string
	}{
		{name: "configured", peers: []ill_db.Peer{peerWithRequestIdPrefix(&configured)}, want: "ABC"},
		{name: "nil", peers: []ill_db.Peer{peerWithRequestIdPrefix(nil)}, want: "REQ1"},
		{name: "empty", peers: []ill_db.Peer{peerWithRequestIdPrefix(&empty)}, want: "REQ1"},
		{name: "no ill config", peers: []ill_db.Peer{{}}, want: "REQ1"},
		{name: "no peer", peers: []ill_db.Peer{}, want: "REQ1"},
		{name: "lookup failure", peers: []ill_db.Peer{}, lookupErr: errors.New("directory down"), want: "REQ1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			illRepo := new(IllRepoMock)
			illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:REQ1"}, mock.Anything).Return(tc.peers, "", tc.lookupErr)
			service := CreatePatronRequestActionService(new(MockPrRepo), illRepo, new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
			got, err := service.RequestIdPrefix(appCtx, "ISIL:REQ1")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRequestIdPrefixInvalidSymbol(t *testing.T) {
	illRepo := new(IllRepoMock)
	service := CreatePatronRequestActionService(new(MockPrRepo), illRepo, new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
	_, err := service.RequestIdPrefix(appCtx, "REQ1")
	require.Error(t, err)
	illRepo.AssertNotCalled(t, "GetCachedPeersBySymbols", mock.Anything, mock.Anything)
}

func TestSuccessorRequestUsesRequestIdPrefix(t *testing.T) {
	prefix := "abc"
	illRepo := new(IllRepoMock)
	illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:REQ1"}, mock.Anything).Return([]ill_db.Peer{peerWithRequestIdPrefix(&prefix)}, "", nil)
	service := CreatePatronRequestActionService(new(MockPrRepo), illRepo, new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
	got := service.createSuccessorBorrowingRequest(appCtx, pr_db.PatronRequest{
		ID:              "previous",
		RequesterSymbol: getDbText("ISIL:REQ1"),
		IllRequest:      iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}},
	}, false)
	require.Empty(t, got.result)
	assert.Equal(t, "ABC-2", got.successorPr.ID)
	assert.Equal(t, "ABC-2", got.successorPr.IllRequest.Header.RequestingAgencyRequestId)
}
