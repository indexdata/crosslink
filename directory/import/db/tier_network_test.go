package importdb

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestRetryableAssignmentImportErrorHandlesBusinessKeyRaces(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "tier business key", err: &pgconn.PgError{Code: "23505", ConstraintName: "tiers_consortium_name_unique"}, want: true},
		{name: "wrapped tier business key", err: fmt.Errorf("persist tier: %w", &pgconn.PgError{Code: "23505", ConstraintName: "tiers_consortium_name_unique"}), want: true},
		{name: "network business key", err: &pgconn.PgError{Code: "23505", ConstraintName: "networks_consortium_name_unique"}, want: true},
		{name: "wrapped network business key", err: fmt.Errorf("persist network: %w", &pgconn.PgError{Code: "23505", ConstraintName: "networks_consortium_name_unique"}), want: true},
		{name: "unrelated unique constraint", err: &pgconn.PgError{Code: "23505", ConstraintName: "entry_tiers_pkey"}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, retryableAssignmentImportError(test.err))
		})
	}
}
