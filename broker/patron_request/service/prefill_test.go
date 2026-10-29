package prservice

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/ill_db"
	lookupservice "github.com/indexdata/crosslink/broker/service"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPrefillCatalogResults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		adapter  *catalog.MockLookupAdapter
		wantErr  error
		wantJSON string
	}{
		{"missing", &catalog.MockLookupAdapter{}, ErrPrefillNotFound, ""},
		{"empty record", &catalog.MockLookupAdapter{RecordFound: true}, nil, `{}`},
		{"metadata without holdings", &catalog.MockLookupAdapter{Metadata: catalog.Metadata{Title: "Title", Publisher: "Publisher", PublicationDate: "[2025]", PublicationType: "Book", Isbn: "9783161484100"}}, nil, `{"bibliographicInfo":{"title":"Title","bibliographicItemId":[{"bibliographicItemIdentifierCode":{"#text":"ISBN"},"bibliographicItemIdentifier":"9783161484100"}]},"publicationInfo":{"publisher":"Publisher","publicationDate":"[2025]","publicationType":{"#text":"Book"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(IllRepoMock)
			// No request repository or event bus is supplied: only peer configuration may be cached.
			repo.On("GetCachedPeersBySymbols", []string{"ISIL:REQ"}, mock.Anything).Return([]ill_db.Peer{{CustomData: dirapi.Entry{}}}, "", nil).Once()
			factory := lookupservice.NewLookupAdapterFactory(repo, nil, "", tc.adapter, nil)
			service := NewPrefillService(repo, nil, factory)
			result, err := service.Prefill(appCtx, "ISIL:REQ", catalog.LookupParams{Identifier: "record"})
			require.ErrorIs(t, err, tc.wantErr)
			if err == nil {
				encoded, err := json.Marshal(result)
				require.NoError(t, err)
				assert.JSONEq(t, tc.wantJSON, string(encoded))
			}
			repo.AssertExpectations(t)
		})
	}
}

func TestPrefillCallerServiceType(t *testing.T) {
	for _, serviceType := range []string{"Loan", "Copy", "CopyOrLoan"} {
		result, err := metadataPrefill(catalog.Metadata{}, catalog.LookupParams{ServiceType: serviceType})
		require.NoError(t, err)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		assert.JSONEq(t, `{"serviceInfo":{"serviceType":"`+serviceType+`"}}`, string(encoded))
	}
}

func TestPrefillLookupFailures(t *testing.T) {
	upstream := errors.New("upstream failed")
	for _, tc := range []struct {
		name    string
		peers   []ill_db.Peer
		repoErr error
		lookup  catalog.LookupAdapter
		want    error
	}{
		{"requester lookup failed", nil, upstream, nil, upstream},
		{"requester missing", nil, nil, nil, ErrPrefillUnavailable},
		{"catalog absent", []ill_db.Peer{{}}, nil, nil, ErrPrefillUnavailable},
		{"catalog lookup failed", []ill_db.Peer{{}}, nil, &catalog.MockLookupAdapter{Err: upstream}, upstream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(IllRepoMock)
			repo.On("GetCachedPeersBySymbols", []string{"ISIL:REQ"}, mock.Anything).Return(tc.peers, "", tc.repoErr).Once()
			factory := lookupservice.NewLookupAdapterFactory(repo, nil, "", tc.lookup, catalog.NewLookupAdapterCreator(catalog.LookupAdapterMock, ""))
			_, err := NewPrefillService(repo, nil, factory).Prefill(appCtx, "ISIL:REQ", catalog.LookupParams{Identifier: "record"})
			require.ErrorIs(t, err, tc.want)
			repo.AssertExpectations(t)
		})
	}
}
