package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostProfilesMigrationPreservesLegacyHoldings(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	exec := func(query string, args ...any) {
		t.Helper()
		_, err := tx.Exec(ctx, query, args...)
		require.NoError(t, err)
	}
	// Build a real pre-009 schema without changing the integration suite's tables.
	exec("CREATE SCHEMA host_profiles_migration_test; SET LOCAL search_path TO host_profiles_migration_test")
	apply := func(version int) {
		t.Helper()
		paths, err := filepath.Glob(fmt.Sprintf("../migrations/%03d_*.up.sql", version))
		require.NoError(t, err)
		require.Len(t, paths, 1)
		data, err := os.ReadFile(paths[0])
		require.NoError(t, err)
		exec(string(data))
	}
	for version := 1; version <= 8; version++ {
		apply(version)
	}

	type legacyHoldings struct {
		name                         string
		marc                         [6]*string
		opac, reservoir, marc21plus1 *bool
		want                         string // Empty means SQL NULL, not an empty JSON object.
	}
	str := func(s string) *string { return &s }
	enabled, disabled := true, false
	cases := []legacyHoldings{
		{name: "unset"},
		{name: "disabled flags", opac: &disabled, reservoir: &disabled, marc21plus1: &disabled},
		{name: "opac", opac: &enabled, reservoir: &disabled, want: `{"opac":{}}`},
		{name: "reservoir", reservoir: &enabled, marc21plus1: &disabled, want: `{"reservoir":{}}`},
		{name: "marc21plus1", marc21plus1: &enabled, opac: &disabled, want: `{"marc21plus1":{}}`},
		{
			name: "all MARC fields",
			marc: [6]*string{str("h"), str("p"), str("b"), str("952"), str("r"), str("c")},
			want: `{"marc":{"callNumberSubField":"h","itemIdSubField":"p","locationSubField":"b","mainField":"952","restrictedSubField":"r","shelvingLocationSubField":"c"}}`,
		},
		// Preserve mixed selections so the broker can retain its precedence:
		// marc, opac, reservoir, marc21plus1. JSON key order has no significance.
		{
			name: "MARC precedes all flags", marc: [6]*string{str("x")},
			opac: &enabled, reservoir: &enabled, marc21plus1: &enabled,
			want: `{"marc":{"callNumberSubField":"x"},"opac":{},"reservoir":{},"marc21plus1":{}}`,
		},
		{
			name: "OPAC precedes other flags", opac: &enabled, reservoir: &enabled, marc21plus1: &enabled,
			want: `{"opac":{},"reservoir":{},"marc21plus1":{}}`,
		},
		{
			name: "reservoir precedes MARC21plus1", opac: &disabled, reservoir: &enabled, marc21plus1: &enabled,
			want: `{"reservoir":{},"marc21plus1":{}}`,
		},
		{name: "explicit empty MARC field", marc: [6]*string{str("")}, want: `{"marc":{"callNumberSubField":""}}`},
	}
	// Each field alone must be enough to preserve MARC selection; missing fields
	// must stay absent so subsequent profile/default merging remains possible.
	for i, field := range []string{"callNumberSubField", "itemIdSubField", "locationSubField", "mainField", "restrictedSubField", "shelvingLocationSubField"} {
		c := legacyHoldings{name: "MARC only " + field, want: fmt.Sprintf(`{"marc":{%q:"x"}}`, field)}
		c.marc[i] = str("x")
		cases = append(cases, c)
	}

	ids := make([]string, len(cases))
	for i, c := range cases {
		ids[i] = uuid.NewString()
		exec("INSERT INTO entries (id, name, type) VALUES ($1, $2, 'institution')", ids[i], c.name)
		exec(`INSERT INTO catalog_configs (
			entry, holdings_marc_call_number_subfield, holdings_marc_item_id_subfield,
			holdings_marc_location_subfield, holdings_marc_main_field,
			holdings_marc_restricted_subfield, holdings_marc_shelving_location_subfield,
			holdings_opac_enabled, holdings_reservoir_enabled, holdings_marc21plus1_enabled
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			ids[i], c.marc[0], c.marc[1], c.marc[2], c.marc[3], c.marc[4], c.marc[5],
			c.opac, c.reservoir, c.marc21plus1)
	}

	apply(9)

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got *string
			require.NoError(t, tx.QueryRow(ctx, "SELECT holdings_config::text FROM catalog_configs WHERE entry=$1", ids[i]).Scan(&got))
			if c.want == "" {
				require.Nil(t, got, "unconfigured parsers must remain SQL NULL")
			} else {
				require.NotNil(t, got)
				require.JSONEq(t, c.want, *got)
			}
		})
	}
}
