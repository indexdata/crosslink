package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPersistRawProfileOverrides(t *testing.T) {
	var cfg CatalogConfig
	require.NoError(t, json.Unmarshal([]byte(`{"profile":"Sierra","holdingsFormat":{"opac":{"requireLocalLocation":false}}}`), &cfg))
	p := catalogConfigToDBParams(uuid.New(), cfg)
	require.Equal(t, "Sierra", *p.Profile)
	require.JSONEq(t, `{"opac":{"requireLocalLocation":false}}`, string(p.HoldingsConfig))
	require.Nil(t, p.ZoomAddress)
	var patch CatalogConfigPatch
	require.NoError(t, json.Unmarshal([]byte(`{"profile":null,"holdingsFormat":{"opac":{"includeItemId":false}}}`), &patch))
	next, err := catalogConfigPatchToDBParams(uuid.New(), patch, db.CatalogConfig{Profile: p.Profile, HoldingsConfig: p.HoldingsConfig})
	require.NoError(t, err)
	require.Nil(t, next.Profile)
	require.JSONEq(t, `{"opac":{"requireLocalLocation":false,"includeItemId":false}}`, string(next.HoldingsConfig))
	patch = CatalogConfigPatch{}
	require.NoError(t, json.Unmarshal([]byte(`{"holdingsFormat":{"marc":{"mainField":"999"}}}`), &patch))
	next, err = catalogConfigPatchToDBParams(uuid.New(), patch, db.CatalogConfig{HoldingsConfig: p.HoldingsConfig})
	require.NoError(t, err)
	require.JSONEq(t, `{"marc":{"mainField":"999"}}`, string(next.HoldingsConfig))
}
