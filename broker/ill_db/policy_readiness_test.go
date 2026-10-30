package ill_db

import (
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidateHoldingsPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		readiness pgtype.Bool
		policy    *dirapi.HoldingsPolicy
		allowed   bool
	}{
		{"legacy without policy", pgtype.Bool{}, nil, true},
		{"legacy with policy", pgtype.Bool{}, &dirapi.HoldingsPolicy{}, false},
		{"pending", pgtype.Bool{Valid: true}, &dirapi.HoldingsPolicy{}, false},
		{"pending before peer resolution", pgtype.Bool{Valid: true}, nil, false},
		{"resolved without item", pgtype.Bool{Valid: true, Bool: true}, &dirapi.HoldingsPolicy{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := LocatedSupplier{HoldingsPolicyResolved: tc.readiness}
			err := selected.ValidateHoldingsPolicy(dirapi.Entry{HoldingsPolicy: tc.policy})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "unresolved")
			}
		})
	}
}
