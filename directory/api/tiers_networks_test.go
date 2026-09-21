package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAddTierRejectsWhitespaceOnlyName(t *testing.T) {
	impl := NewApiImpl(nil, nil, nil)
	response, err := impl.AddTier(consortialAdminContext(t), AddTierRequestObject{
		Body: &Tier{Consortium: uuid.New(), Name: " \t\n"},
	})

	require.NoError(t, err)
	require.IsType(t, AddTier400TextResponse(""), response)
}

func TestTierTypeAcceptsCopyOrLoan(t *testing.T) {
	require.True(t, Copyorloan.Valid())
}

func TestAddNetworkRejectsWhitespaceOnlyName(t *testing.T) {
	impl := NewApiImpl(nil, nil, nil)
	response, err := impl.AddNetwork(consortialAdminContext(t), AddNetworkRequestObject{
		Body: &Network{Consortium: uuid.New(), Name: " \t\n"},
	})

	require.NoError(t, err)
	require.IsType(t, AddNetwork400TextResponse(""), response)
}
